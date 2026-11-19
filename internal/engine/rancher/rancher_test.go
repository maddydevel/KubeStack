// Copyright 2026 The KubeStack Authors. All rights reserved.

package rancher

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
	"kubestack.dev/kubestack/internal/upgrade"
)

func u(apiVersion, kind, ns, name string, labels map[string]any, fields map[string]any) *unstructured.Unstructured {
	md := map[string]any{"name": name, "labels": labels}
	if ns != "" {
		md["namespace"] = ns
	}
	obj := map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": md}
	for k, v := range fields {
		obj[k] = v
	}
	return &unstructured.Unstructured{Object: obj}
}

func newEngine(objs ...runtime.Object) *Engine {
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		provGVR: "ClusterList", mgmtGVR: "ClusterList", snapGVR: "ETCDSnapshotList", machineGVR: "MachineList", crtGVR: "ClusterRegistrationTokenList", settingGVR: "SettingList", configMapGVR: "ConfigMapList",
	}, objs...)
	return NewForClient(c)
}

func TestClassifyAndLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	e := newEngine(
		u("provisioning.cattle.io/v1", "Cluster", namespace, "rke", nil, map[string]any{
			"spec":   map[string]any{"kubernetesVersion": "v1.34.12+rke2r1", "rkeConfig": map[string]any{}},
			"status": map[string]any{"ready": true, "clusterName": "c-m-1"},
		}),
		u("provisioning.cattle.io/v1", "Cluster", namespace, "eks", nil, map[string]any{
			"status": map[string]any{"clusterName": "c-2"},
		}),
		u("management.cattle.io/v3", "Cluster", "", "c-2", nil, map[string]any{
			"spec":   map[string]any{"eksConfig": map[string]any{"region": "eu-west-1"}},
			"status": map[string]any{"version": map[string]any{"gitVersion": "v1.33.4-eks"}},
		}),
		u("provisioning.cattle.io/v1", "Cluster", namespace, "imported", nil, nil),
		u("rke.cattle.io/v1", "ETCDSnapshot", namespace, "s1", map[string]any{"rke.cattle.io/cluster-name": "rke"}, map[string]any{
			"snapshotFile": map[string]any{"status": "successful", "createdAt": now.Format(time.RFC3339)},
		}),
		u("rke.cattle.io/v1", "ETCDSnapshot", namespace, "s2", map[string]any{"rke.cattle.io/cluster-name": "rke"}, map[string]any{
			"snapshotFile": map[string]any{"status": "failed", "createdAt": now.Add(time.Hour).Format(time.RFC3339)},
		}),
		u("cluster.x-k8s.io/v1beta2", "Machine", namespace, "m1", map[string]any{"cluster.x-k8s.io/cluster-name": "rke", "rke.cattle.io/etcd-role": "true", "rke.cattle.io/control-plane-role": "true"}, map[string]any{
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
		}),
		u("cluster.x-k8s.io/v1beta2", "Machine", namespace, "m2", map[string]any{"cluster.x-k8s.io/cluster-name": "rke"}, map[string]any{
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False"}}},
		}),
	)

	want := map[string]domain.Tier{"rke": domain.TierProvisioned, "eks": domain.TierHosted, "imported": domain.TierAttached}
	cs, err := e.ListClusters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if want[c.ID] != c.Tier {
			t.Errorf("%s: tier %s, want %s", c.ID, c.Tier, want[c.ID])
		}
	}
	if c, _ := e.GetCluster(ctx, "eks"); c.UpgradeOwner != domain.OwnerCloud || c.KubernetesVersion != "v1.33.4-eks" {
		t.Errorf("eks: %+v", c)
	}

	snaps, err := e.Snapshots(ctx, "rke")
	if err != nil || len(snaps) != 2 {
		t.Fatalf("snapshots: %+v %v", snaps, err)
	}
	if p := upgrade.PreflightFrom(snaps, nil); !p.LatestSnapshot.Equal(now) {
		t.Errorf("latest successful snapshot = %v, want %v (failed snapshots ignored)", p.LatestSnapshot, now)
	}
	nodes, _ := e.Nodes(ctx, "rke")
	if p := upgrade.PreflightFrom(nil, nodes); p.ReadyNodes != 1 || p.TotalNodes != 2 {
		t.Errorf("node health = %d/%d, want 1/2", p.ReadyNodes, p.TotalNodes)
	}
	if nodes[0].Name != "m1" || len(nodes[0].Roles) != 2 || nodes[0].Roles[0] != "etcd" {
		t.Errorf("roles from labels: %+v", nodes[0])
	}

	if err := e.CreateSnapshot(ctx, "rke"); err != nil {
		t.Fatal(err)
	}
	if err := e.RestoreSnapshot(ctx, "rke", "s1"); err != nil {
		t.Fatal(err)
	}
	p, _ := e.prov().Get(ctx, "rke", metav1.GetOptions{})
	if g, _, _ := unstructured.NestedInt64(p.Object, "spec", "rkeConfig", "etcdSnapshotCreate", "generation"); g != 1 {
		t.Errorf("etcdSnapshotCreate.generation = %d", g)
	}
	if n, _, _ := unstructured.NestedString(p.Object, "spec", "rkeConfig", "etcdSnapshotRestore", "name"); n != "s1" {
		t.Errorf("etcdSnapshotRestore.name = %q", n)
	}
	if err := e.RemoveNode(ctx, "other", "m2"); !errors.Is(err, engine.ErrNotFound) {
		t.Errorf("removing another cluster's machine: %v", err)
	}
	if err := e.RemoveNode(ctx, "rke", "m2"); err != nil {
		t.Fatal(err)
	}

	if err := e.UpgradeCluster(ctx, "rke", "v1.35.9+rke2r1"); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.GetCluster(ctx, "rke"); c.KubernetesVersion != "v1.35.9+rke2r1" {
		t.Errorf("upgrade not applied: %+v", c)
	}

	created, err := e.CreateCluster(ctx, domain.CreateRequest{Name: "new", KubernetesVersion: "v1.35.9+rke2r1"})
	if err != nil || created.Tier != domain.TierProvisioned {
		t.Fatalf("create: %+v %v", created, err)
	}
	if _, err := e.CreateCluster(ctx, domain.CreateRequest{Name: "new"}); err == nil {
		t.Error("duplicate create must fail")
	}
	if err := e.DeleteCluster(ctx, "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.GetCluster(ctx, "new"); err == nil {
		t.Error("deleted cluster still found")
	}
}

func crt(ns, secret, field, cmd string) *unstructured.Unstructured {
	return u("management.cattle.io/v3", "ClusterRegistrationToken", ns, "default-token", nil, map[string]any{
		"status": map[string]any{"tokenSecretName": secret, field: cmd, "token": "must-never-be-read"},
	})
}

func TestRegistration(t *testing.T) {
	ctx := context.Background()
	e := newEngine(
		u("provisioning.cattle.io/v1", "Cluster", namespace, "rke", nil, map[string]any{
			"spec": map[string]any{"rkeConfig": map[string]any{}}, "status": map[string]any{"clusterName": "c-m-1"}}),
		crt("c-m-1", "crt-token-abc", "nodeCommand", "curl -fL https://r/system-agent-install.sh | sudo sh -s - --token {token}"),
		u("provisioning.cattle.io/v1", "Cluster", namespace, "imp", nil, map[string]any{"status": map[string]any{"clusterName": "c-2"}}),
		crt("c-2", "crt-token-def", "command", "kubectl apply -f https://r/v3/import/{token}_c-2.yaml"),
		u("provisioning.cattle.io/v1", "Cluster", namespace, "leaky", nil, map[string]any{
			"spec": map[string]any{"rkeConfig": map[string]any{}}, "status": map[string]any{"clusterName": "c-3"}}),
		crt("c-3", "crt-token-x", "nodeCommand", "curl ... --token s3cr3t"),
		u("provisioning.cattle.io/v1", "Cluster", namespace, "fresh", nil, nil),
	)
	r, err := e.Registration(ctx, "rke")
	if err != nil || !strings.Contains(r.Command, "system-agent-install") || r.Token != (domain.SecretRef{Namespace: "c-m-1", Name: "crt-token-abc", Key: "token"}) {
		t.Errorf("provisioned: %+v %v", r, err)
	}
	if r, err := e.Registration(ctx, "imp"); err != nil || !strings.HasPrefix(r.Command, "kubectl apply") {
		t.Errorf("imported: %+v %v", r, err)
	}
	if r, err := e.Registration(ctx, "leaky"); err == nil || strings.Contains(r.Command+err.Error(), "s3cr3t") {
		t.Errorf("command with inlined token must be refused without echoing it: %+v %v", r, err)
	}
	if _, err := e.Registration(ctx, "fresh"); !errors.Is(err, engine.ErrNotReady) {
		t.Errorf("unlinked cluster: %v", err)
	}
	imp, err := e.ImportCluster(ctx, domain.ImportRequest{Name: "brownfield"})
	if err != nil || imp.Tier != domain.TierAttached || imp.UpgradeOwner != domain.OwnerNone {
		t.Errorf("import: %+v %v", imp, err)
	}
}

func TestCreateWithOIDCAndAccess(t *testing.T) {
	ctx := context.Background()
	e := newEngine(
		u("provisioning.cattle.io/v1", "Cluster", namespace, "rke", nil, map[string]any{
			"spec": map[string]any{"rkeConfig": map[string]any{}}, "status": map[string]any{"clusterName": "c-m-1"}}),
		u("management.cattle.io/v3", "Cluster", "", "c-m-1", nil, map[string]any{"status": map[string]any{"caCert": "Q0E="}}),
		u("cluster.x-k8s.io/v1beta2", "Machine", namespace, "cp", map[string]any{"cluster.x-k8s.io/cluster-name": "rke", "rke.cattle.io/control-plane-role": "true"},
			map[string]any{"status": map[string]any{"addresses": []any{
				map[string]any{"type": "InternalIP", "address": "10.0.0.5"}, map[string]any{"type": "ExternalIP", "address": "203.0.113.5"}}}}),
		u("cluster.x-k8s.io/v1beta2", "Machine", namespace, "w", map[string]any{"cluster.x-k8s.io/cluster-name": "rke", "rke.cattle.io/worker-role": "true"},
			map[string]any{"status": map[string]any{"addresses": []any{map[string]any{"type": "InternalIP", "address": "10.0.0.9"}}}}),
		u("provisioning.cattle.io/v1", "Cluster", namespace, "fresh", nil, map[string]any{"spec": map[string]any{"rkeConfig": map[string]any{}}}),
	)
	a, err := e.Access(ctx, "rke")
	if err != nil || len(a.Servers) != 1 || a.Servers[0] != "https://203.0.113.5:6443" || a.CertificateAuthorityData != "Q0E=" {
		t.Fatalf("access (control plane only, external IP preferred): %+v %v", a, err)
	}
	if _, err := e.Access(ctx, "fresh"); !errors.Is(err, engine.ErrNotReady) {
		t.Fatalf("no nodes: %v", err)
	}

	oidc := &domain.ClusterOIDC{IssuerURL: "https://idp.example", ClientID: "kubestack", GroupsClaim: "groups", AdminGroup: "ops"}
	if _, err := e.CreateCluster(ctx, domain.CreateRequest{Name: "o", KubernetesVersion: "v1.35.9+rke2r1", OIDC: oidc}); err != nil {
		t.Fatal(err)
	}
	p, _ := e.prov().Get(ctx, "o", metav1.GetOptions{})
	args, _, _ := unstructured.NestedStringSlice(p.Object, "spec", "rkeConfig", "machineGlobalConfig", "kube-apiserver-arg")
	if !slices.Contains(args, "oidc-issuer-url=https://idp.example") || !slices.Contains(args, "oidc-groups-prefix=oidc:") {
		t.Errorf("apiserver args: %v", args)
	}
	m, _, _ := unstructured.NestedString(p.Object, "spec", "rkeConfig", "additionalManifest")
	if !strings.Contains(m, `name: "oidc:ops"`) || !strings.Contains(m, "cluster-admin") {
		t.Errorf("admin binding manifest: %s", m)
	}
}

func TestImportedUpgradeOwner(t *testing.T) {
	ctx := context.Background()
	imported := func(name, mid string) *unstructured.Unstructured {
		return u("provisioning.cattle.io/v1", "Cluster", namespace, name, nil, map[string]any{"status": map[string]any{"clusterName": mid}})
	}
	mgmt := func(mid, driver, anno string) *unstructured.Unstructured {
		m := u("management.cattle.io/v3", "Cluster", "", mid, nil, map[string]any{"status": map[string]any{"driver": driver}})
		if anno != "" {
			m.SetAnnotations(map[string]string{versionMgmtAnno: anno})
		}
		return m
	}
	setting := u("management.cattle.io/v3", "Setting", "", versionMgmtSetting, nil, map[string]any{"value": "true"})
	e := newEngine(setting,
		imported("rke2-default", "c-1"), mgmt("c-1", "rke2", ""),
		imported("k3s-optout", "c-2"), mgmt("c-2", "k3s", "false"),
		imported("generic", "c-3"), mgmt("c-3", "imported", "true"),
	)
	want := map[string]domain.UpgradeOwner{"rke2-default": domain.OwnerRancher, "k3s-optout": domain.OwnerNone, "generic": domain.OwnerNone}
	for id, owner := range want {
		if c, err := e.GetCluster(ctx, id); err != nil || c.UpgradeOwner != owner || c.Tier != domain.TierAttached {
			t.Errorf("%s: %+v %v, want owner %s", id, c, err, owner)
		}
	}
	// Without the setting (e.g. no RBAC), only an explicit annotation counts.
	e = newEngine(imported("rke2-default", "c-1"), mgmt("c-1", "rke2", ""), imported("rke2-on", "c-4"), mgmt("c-4", "rke2", "true"))
	if c, _ := e.GetCluster(ctx, "rke2-default"); c.UpgradeOwner != domain.OwnerNone {
		t.Errorf("default without setting: %+v", c)
	}
	if c, _ := e.GetCluster(ctx, "rke2-on"); c.UpgradeOwner != domain.OwnerRancher {
		t.Errorf("annotation true: %+v", c)
	}
}

func TestCreateWithOIDCCA(t *testing.T) {
	ctx := context.Background()
	e := newEngine()
	ca := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")
	oidc := &domain.ClusterOIDC{IssuerURL: "https://idp.example", ClientID: "k", GroupsClaim: "groups", AdminGroup: "ops", CAPEM: ca}
	if _, err := e.CreateCluster(ctx, domain.CreateRequest{Name: "c", KubernetesVersion: "v1.35.9+rke2r1", OIDC: oidc}); err != nil {
		t.Fatal(err)
	}
	p, _ := e.prov().Get(ctx, "c", metav1.GetOptions{})
	args, _, _ := unstructured.NestedStringSlice(p.Object, "spec", "rkeConfig", "machineGlobalConfig", "kube-apiserver-arg")
	mounts, _, _ := unstructured.NestedStringSlice(p.Object, "spec", "rkeConfig", "machineGlobalConfig", "kube-apiserver-extra-mount")
	files, _, _ := unstructured.NestedSlice(p.Object, "spec", "rkeConfig", "machineSelectorFiles")
	if !slices.Contains(args, "oidc-ca-file=/etc/kubestack/oidc-ca.crt") || len(mounts) != 1 || len(files) != 1 {
		t.Fatalf("args %v mounts %v files %v", args, mounts, files)
	}
	cm, err := e.c.Resource(configMapGVR).Namespace(namespace).Get(ctx, "kubestack-oidc-ca-c", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cm.GetAnnotations()["rke.cattle.io/object-authorized-for-clusters"] != "c" || len(cm.GetOwnerReferences()) != 1 {
		t.Errorf("configmap metadata: %v %v", cm.GetAnnotations(), cm.GetOwnerReferences())
	}
	if d, _, _ := unstructured.NestedString(cm.Object, "data", "ca.crt"); d != string(ca) {
		t.Errorf("ca data: %q", d)
	}
}

func TestBumpGenerationConflicts(t *testing.T) {
	ctx := context.Background()
	e := newEngine(u("provisioning.cattle.io/v1", "Cluster", namespace, "rke", nil, map[string]any{"spec": map[string]any{"rkeConfig": map[string]any{}}}))
	fc := e.c.(*dynamicfake.FakeDynamicClient)
	conflict := apierrors.NewConflict(schema.GroupResource{Group: "provisioning.cattle.io", Resource: "clusters"}, "rke", errors.New("modified"))

	// An unrelated concurrent write: one conflict, then the patch succeeds.
	n := 0
	fc.PrependReactor("patch", "clusters", func(clienttesting.Action) (bool, runtime.Object, error) {
		n++
		return n == 1, nil, conflict
	})
	if err := e.CreateSnapshot(ctx, "rke"); err != nil || n != 2 {
		t.Fatalf("unrelated conflict must be retried: %v (patches %d)", err, n)
	}

	// Someone else bumped the same generation between our read and write: refuse, don't double it.
	e = newEngine(u("provisioning.cattle.io/v1", "Cluster", namespace, "rke", nil, map[string]any{"spec": map[string]any{"rkeConfig": map[string]any{}}}))
	fc = e.c.(*dynamicfake.FakeDynamicClient)
	other := false
	fc.PrependReactor("patch", "clusters", func(clienttesting.Action) (bool, runtime.Object, error) {
		if other {
			return false, nil, nil
		}
		other = true
		// The fake client holds its lock while reactors run, so go through the tracker directly.
		obj, _ := fc.Tracker().Get(provGVR, namespace, "rke")
		p := obj.(*unstructured.Unstructured)
		_ = unstructured.SetNestedField(p.Object, int64(1), "spec", "rkeConfig", "etcdSnapshotCreate", "generation")
		_ = fc.Tracker().Update(provGVR, p, namespace)
		return true, nil, conflict
	})
	if err := e.CreateSnapshot(ctx, "rke"); !errors.Is(err, engine.ErrConflict) {
		t.Fatalf("concurrent same-operation request must be refused: %v", err)
	}
}
