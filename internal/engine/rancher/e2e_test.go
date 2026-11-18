// Copyright 2026 The KubeStack Authors. All rights reserved.

//go:build e2e

// E2E against a real community Rancher (see hack/e2e.sh). Verifies the CRD field semantics the
// adapter relies on: create is accepted by the Rancher webhook, Rancher links a management cluster
// via status.clusterName, upgrade patches are accepted, snapshot/machine queries work, delete completes.
package rancher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"

	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
	"kubestack.dev/kubestack/internal/upgrade"
)

func eventually(t *testing.T, timeout time.Duration, what string, f func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, err := f()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (last err: %v)", what, err)
		}
		time.Sleep(2 * time.Second)
	}
}

func TestE2E(t *testing.T) {
	ctx := context.Background()
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Use the RKE2 version this Rancher release defaults to, so the test tracks the pinned version.
	setting, err := e.c.Resource(schema.GroupVersionResource{Group: "management.cattle.io", Version: "v3", Resource: "settings"}).
		Get(ctx, "rke2-default-version", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	version, _, _ := unstructured.NestedString(setting.Object, "value")
	if version == "" {
		version, _, _ = unstructured.NestedString(setting.Object, "default")
	}
	if version != "" && version[0] != 'v' {
		version = "v" + version // the setting omits the "v" the webhook requires
	}
	t.Logf("Rancher rke2-default-version = %q", version)
	// Rancher's default depends on whether it can refresh release metadata online, so only a missing
	// minor fails; patch drift is a refresh hint. The cluster is created with what KubeStack would send.
	catalog := upgrade.Versions()
	minor := func(v string) string { p := strings.SplitN(v, ".", 3); return p[0] + "." + p[1] }
	if !slices.ContainsFunc(catalog, func(c string) bool { return minor(c) == minor(version) }) {
		t.Errorf("bundled release catalog %v has no %s release (Rancher default %s): refresh internal/upgrade/releases.json", catalog, minor(version), version)
	} else if !slices.Contains(catalog, version) {
		t.Logf("catalog drift: Rancher default %s is not in the bundled catalog %v", version, catalog)
	}
	version = catalog[len(catalog)-1]

	// Registration commands need Rancher's server URL; a fresh install has none.
	const serverURL = "https://engine.kubestack-e2e.test"
	if _, err := e.c.Resource(schema.GroupVersionResource{Group: "management.cattle.io", Version: "v3", Resource: "settings"}).
		Patch(ctx, "server-url", types.MergePatchType, []byte(`{"value":"`+serverURL+`"}`), metav1.PatchOptions{}); err != nil {
		t.Fatalf("set server-url: %v", err)
	}

	name := fmt.Sprintf("ks-e2e-%d", time.Now().Unix()%100000)
	oidc := &domain.ClusterOIDC{IssuerURL: "https://idp.kubestack-e2e.test", ClientID: "kubestack", GroupsClaim: "groups", AdminGroup: "e2e-admins"}
	c, err := e.CreateCluster(ctx, domain.CreateRequest{Name: name, KubernetesVersion: version, OIDC: oidc})
	if err != nil {
		t.Fatalf("create (webhook must accept traefik, kube-apiserver OIDC args and the admin binding manifest): %v", err)
	}
	t.Cleanup(func() { _ = e.DeleteCluster(context.Background(), name) })
	if c.Tier != domain.TierProvisioned || c.UpgradeOwner != domain.OwnerRancher {
		t.Fatalf("created cluster classified as %+v", c)
	}

	eventually(t, 2*time.Minute, "status.clusterName (management cluster link)", func() (bool, error) {
		p, err := e.prov().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		id, _, _ := unstructured.NestedString(p.Object, "status", "clusterName")
		return id != "", nil
	})

	// Rancher must have created the CAPI Cluster owner; without it deletion can never finish.
	capiGVR := schema.GroupVersionResource{Group: "cluster.x-k8s.io", Version: "v1beta2", Resource: "clusters"}
	eventually(t, 2*time.Minute, "CAPI Cluster created by Rancher", func() (bool, error) {
		_, err := e.c.Resource(capiGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		return err == nil, err
	})

	// Node registration: command with placeholder, token in a Secret we only reference.
	var reg domain.Registration
	eventually(t, 2*time.Minute, "registration command", func() (bool, error) {
		reg, err = e.Registration(ctx, name)
		return err == nil, err
	})
	if !strings.Contains(reg.Command, "--server "+serverURL) || !strings.Contains(reg.Command, "--token "+domain.TokenPlaceholder) {
		t.Errorf("node command not in the expected form: %q", reg.Command)
	}
	checkTokenSecret(t, e, reg.Token)

	got, err := e.GetCluster(ctx, name)
	if err != nil || got.Tier != domain.TierProvisioned || got.KubernetesVersion != version {
		t.Fatalf("get after mgmt link: %+v %v", got, err)
	}
	all, err := e.ListClusters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range all {
		found = found || x.ID == name
		if x.ID == "local" {
			t.Error("Rancher's local cluster must not be exposed")
		}
	}
	if !found {
		t.Fatal("created cluster missing from list")
	}

	// No nodes registered: queries must succeed and report nothing (pre-flight would then refuse).
	if snaps, err := e.Snapshots(ctx, name); err != nil || len(snaps) != 0 {
		t.Fatalf("snapshot query: %v %v", snaps, err)
	}
	if nodes, err := e.Nodes(ctx, name); err != nil || len(nodes) != 0 {
		t.Fatalf("node query: %v %v", nodes, err)
	}
	if _, err := e.Access(ctx, name); !errors.Is(err, engine.ErrNotReady) {
		t.Fatalf("access without nodes: %v", err)
	}
	p, _ := e.prov().Get(ctx, name, metav1.GetOptions{})
	if args, _, _ := unstructured.NestedStringSlice(p.Object, "spec", "rkeConfig", "machineGlobalConfig", "kube-apiserver-arg"); !slices.Contains(args, "oidc-issuer-url="+oidc.IssuerURL) {
		t.Fatalf("OIDC args not persisted: %v", args)
	}
	// The webhook must accept the one-shot etcd operations (no nodes, so Rancher can't act on them).
	if err := e.CreateSnapshot(ctx, name); err != nil {
		t.Fatalf("etcdSnapshotCreate rejected: %v", err)
	}
	if err := e.RestoreSnapshot(ctx, name, name+"-etcd-snapshot-e2e"); err != nil {
		t.Fatalf("etcdSnapshotRestore rejected: %v", err)
	}

	if err := e.UpgradeCluster(ctx, name, version); err != nil {
		t.Fatalf("upgrade patch rejected: %v", err)
	}

	// Import an existing cluster: tier attached, manifest command with placeholder, disconnect works.
	imp := name + "-imp"
	ic, err := e.ImportCluster(ctx, domain.ImportRequest{Name: imp})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Cleanup(func() { _ = e.DeleteCluster(context.Background(), imp) })
	if ic.Tier != domain.TierAttached || ic.UpgradeOwner != domain.OwnerNone {
		t.Fatalf("imported cluster classified as %+v", ic)
	}
	// The setting that decides who owns imported RKE2/K3s upgrades must exist under this name.
	if _, err := e.c.Resource(settingGVR).Get(ctx, versionMgmtSetting, metav1.GetOptions{}); err != nil {
		t.Fatalf("setting %s: %v", versionMgmtSetting, err)
	}
	eventually(t, 2*time.Minute, "import command", func() (bool, error) {
		reg, err = e.Registration(ctx, imp)
		return err == nil, err
	})
	if !strings.HasPrefix(reg.Command, "kubectl apply -f "+serverURL+"/v3/import/"+domain.TokenPlaceholder+"_") {
		t.Errorf("import command not in the expected form: %q", reg.Command)
	}
	checkTokenSecret(t, e, reg.Token)
	if err := e.DeleteCluster(ctx, imp); err != nil {
		t.Fatal(err)
	}

	if err := e.DeleteCluster(ctx, name); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{name, imp} {
		eventually(t, 3*time.Minute, "deletion of "+n, func() (bool, error) {
			_, err := e.GetCluster(ctx, n)
			return errors.Is(err, engine.ErrNotFound), err
		})
	}
}

// checkTokenSecret verifies the reference resolves to a non-empty token (the test runs as admin;
// the product API has no Secret access). The value is never printed.
func checkTokenSecret(t *testing.T, e *Engine, ref domain.SecretRef) {
	t.Helper()
	sec, err := e.c.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace(ref.Namespace).
		Get(context.Background(), ref.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("token secret %s/%s: %v", ref.Namespace, ref.Name, err)
	}
	if v, _, _ := unstructured.NestedString(sec.Object, "data", ref.Key); v == "" {
		t.Fatalf("token secret %s/%s has no %q", ref.Namespace, ref.Name, ref.Key)
	}
}
