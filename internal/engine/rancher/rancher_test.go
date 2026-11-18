// Copyright 2026 The KubeStack Authors. All rights reserved.

package rancher

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"kubestack.dev/kubestack/internal/domain"
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
		provGVR: "ClusterList", mgmtGVR: "ClusterList", snapGVR: "ETCDSnapshotList", machineGVR: "MachineList",
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
		u("cluster.x-k8s.io/v1beta2", "Machine", namespace, "m1", map[string]any{"cluster.x-k8s.io/cluster-name": "rke"}, map[string]any{
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

	snap, err := e.LatestSnapshot(ctx, "rke")
	if err != nil || !snap.Equal(now) {
		t.Errorf("snapshot = %v, %v; want %v (failed snapshots ignored)", snap, err, now)
	}
	if r, n, _ := e.NodeHealth(ctx, "rke"); r != 1 || n != 2 {
		t.Errorf("node health = %d/%d, want 1/2", r, n)
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
