// Copyright 2026 The KubeStack Authors. All rights reserved.

package upgrade

import (
	"testing"
	"time"

	"kubestack.dev/kubestack/internal/domain"
)

func TestValidateSkew(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{"v1.34.12+rke2r1", "v1.35.9+rke2r1", true},
		{"v1.34.1+rke2r1", "v1.34.12+rke2r1", true},
		{"v1.34.12+rke2r1", "v1.36.5+rke2r1", false}, // skips 1.35
		{"v1.35.9+rke2r1", "v1.34.12+rke2r1", false}, // downgrade
		{"v1.35.9+rke2r1", "v1.35.1+rke2r1", false},  // patch downgrade
		{"v1.35.9+rke2r1", "v1.35.9+rke2r1", false},  // no-op
		{"garbage", "v1.35.9+rke2r1", false},
	}
	for _, c := range cases {
		if err := ValidateSkew(c.from, c.to); (err == nil) != c.ok {
			t.Errorf("%s -> %s: err=%v, want ok=%v", c.from, c.to, err, c.ok)
		}
	}
}

func TestCheck(t *testing.T) {
	now := time.Now()
	c := domain.Cluster{Tier: domain.TierProvisioned, UpgradeOwner: domain.OwnerRancher, KubernetesVersion: "v1.34.12+rke2r1", Ready: true}
	good := Preflight{LatestSnapshot: now.Add(-time.Hour), ReadyNodes: 3, TotalNodes: 3}

	if f := Check(c, "v1.35.9+rke2r1", good, now); len(f) != 0 {
		t.Fatalf("expected pass, got %v", f)
	}
	stale := good
	stale.LatestSnapshot = now.Add(-48 * time.Hour)
	stale.ReadyNodes = 2
	if f := Check(c, "v1.35.9+rke2r1", stale, now); len(f) != 2 {
		t.Fatalf("expected snapshot+node failures, got %v", f)
	}
	if f := Check(c, "v1.35.9+rke2r1", Preflight{}, now); len(f) != 2 {
		t.Fatalf("expected no-snapshot+no-nodes failures, got %v", f)
	}
	hosted := c
	hosted.Tier, hosted.UpgradeOwner = domain.TierHosted, domain.OwnerCloud
	if f := Check(hosted, "v1.35.9+rke2r1", good, now); len(f) != 1 {
		t.Fatalf("hosted cluster must be refused, got %v", f)
	}
	kubestackOwned := c
	kubestackOwned.UpgradeOwner = domain.OwnerKubeStack
	if f := Check(kubestackOwned, "v1.35.9+rke2r1", good, now); len(f) != 1 {
		t.Fatalf("SUC path is not implemented and must be refused, got %v", f)
	}
}

func TestCatalogParses(t *testing.T) {
	for _, v := range Versions() {
		if _, err := parse(v); err != nil {
			t.Error(err)
		}
	}
}
