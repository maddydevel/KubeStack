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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	if !slices.Contains(upgrade.Versions(), version) {
		t.Errorf("bundled release catalog %v lacks this Rancher's default RKE2 %s: refresh internal/upgrade/releases.json", upgrade.Versions(), version)
	}

	name := fmt.Sprintf("ks-e2e-%d", time.Now().Unix()%100000)
	c, err := e.CreateCluster(ctx, domain.CreateRequest{Name: name, KubernetesVersion: version})
	if err != nil {
		t.Fatalf("create (webhook must accept rkeConfig + ingress-controller=traefik): %v", err)
	}
	t.Cleanup(func() { e.DeleteCluster(context.Background(), name) })
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
	if snap, err := e.LatestSnapshot(ctx, name); err != nil || !snap.IsZero() {
		t.Fatalf("snapshot query: %v %v", snap, err)
	}
	if r, n, err := e.NodeHealth(ctx, name); err != nil || r != 0 || n != 0 {
		t.Fatalf("node health query: %d/%d %v", r, n, err)
	}

	if err := e.UpgradeCluster(ctx, name, version); err != nil {
		t.Fatalf("upgrade patch rejected: %v", err)
	}

	if err := e.DeleteCluster(ctx, name); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Minute, "cluster deletion", func() (bool, error) {
		_, err := e.GetCluster(ctx, name)
		return errors.Is(err, engine.ErrNotFound), err
	})
}
