// Copyright 2026 The KubeStack Authors. All rights reserved.

//go:build e2e

// E2E against a real community Rancher (see hack/e2e.sh). Verifies the CRD field semantics the
// adapter relies on: create is accepted by the Rancher webhook, Rancher links a management cluster
// via status.clusterName, upgrade patches are accepted, snapshot/machine queries work, delete completes.
package rancher

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	serverURL := "https://engine.kubestack-e2e.test"
	if u := os.Getenv("KUBESTACK_E2E_SERVER_URL"); u != "" {
		serverURL = u // reachable by the real imported cluster
	}
	if _, err := e.c.Resource(schema.GroupVersionResource{Group: "management.cattle.io", Version: "v3", Resource: "settings"}).
		Patch(ctx, "server-url", types.MergePatchType, []byte(`{"value":"`+serverURL+`"}`), metav1.PatchOptions{}); err != nil {
		t.Fatalf("set server-url: %v", err)
	}

	name := fmt.Sprintf("ks-e2e-%d", time.Now().Unix()%100000)
	oidc := &domain.ClusterOIDC{IssuerURL: "https://idp.kubestack-e2e.test", ClientID: "kubestack", GroupsClaim: "groups", AdminGroup: "e2e-admins",
		CAPEM: []byte("-----BEGIN CERTIFICATE-----\ne2e\n-----END CERTIFICATE-----\n")}
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
	if args, _, _ := unstructured.NestedStringSlice(p.Object, "spec", "rkeConfig", "machineGlobalConfig", "kube-apiserver-arg"); !slices.Contains(args, "oidc-issuer-url="+oidc.IssuerURL) || !slices.Contains(args, "oidc-ca-file="+oidcCAPath) {
		t.Fatalf("OIDC args not persisted: %v", args)
	}
	if _, err := e.c.Resource(configMapGVR).Namespace(namespace).Get(ctx, oidcCAConfigMap(name), metav1.GetOptions{}); err != nil {
		t.Fatalf("IdP CA configmap: %v", err)
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
	if kc := os.Getenv("KUBESTACK_E2E_IMPORT_KUBECONFIG"); kc != "" {
		connectRealCluster(t, e, reg, kc)
		eventually(t, 8*time.Minute, "imported K3s cluster connected and ready", func() (bool, error) {
			c, err := e.GetCluster(ctx, imp)
			return err == nil && c.Ready && c.KubernetesVersion != "", err
		})
		c, _ := e.GetCluster(ctx, imp)
		t.Logf("imported cluster: %+v", c)
		// Rancher v2.15 enables imported-cluster version management by default, so it owns K3s upgrades.
		if c.Tier != domain.TierAttached || c.UpgradeOwner != domain.OwnerRancher || !strings.Contains(c.KubernetesVersion, "k3s") {
			t.Errorf("real imported K3s classified as %+v", c)
		}
		if ok, why := c.Allowed(domain.OpUpgrade); ok || why == "" {
			t.Errorf("upgrade of imported cluster must be refused with a reason: %v %q", ok, why)
		}
	}
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
	eventually(t, 2*time.Minute, "IdP CA configmap garbage-collected with its cluster", func() (bool, error) {
		_, err := e.c.Resource(configMapGVR).Namespace(namespace).Get(ctx, oidcCAConfigMap(name), metav1.GetOptions{})
		return apierrors.IsNotFound(err), err
	})
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

// connectRealCluster does what an operator does with the registration command: read the token from
// the referenced Secret, substitute it, and kubectl-apply the manifest to the existing cluster. The
// manifest is fetched with TLS verification off because the e2e Rancher uses its self-signed CA (the
// agent itself pins that CA by checksum); kubectl in the Rancher container applies it.
func connectRealCluster(t *testing.T, e *Engine, reg domain.Registration, kubeconfig string) {
	t.Helper()
	sec, err := e.c.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace(reg.Token.Namespace).
		Get(context.Background(), reg.Token.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b64, _, _ := unstructured.NestedString(sec.Object, "data", reg.Token.Key)
	token, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	url := strings.Fields(strings.ReplaceAll(reg.Command, domain.TokenPlaceholder, string(token)))[3]                                     // kubectl apply -f <url>
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // #nosec G402 -- e2e only, see above
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal("fetch import manifest failed") // the URL embeds the token; don't print it
	}
	defer resp.Body.Close()
	manifest, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch import manifest: HTTP %d", resp.StatusCode)
	}
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(manifest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("kubectl apply import manifest: %v\n%s", err, out)
	}
}
