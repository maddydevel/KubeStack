// Copyright 2026 The KubeStack Authors. All rights reserved.

package domain

import "testing"

func TestRemovalBlocked(t *testing.T) {
	nodes := []Node{
		{Name: "cp1", Roles: []string{"etcd", "controlplane"}},
		{Name: "w1", Roles: []string{"worker"}},
		{Name: "w2", Roles: []string{"worker"}},
	}
	for name, blocked := range map[string]bool{"cp1": true, "w1": false, "missing": true} {
		if got := RemovalBlocked(nodes, name) != ""; got != blocked {
			t.Errorf("%s: blocked=%v want %v", name, got, blocked)
		}
	}
	nodes = append(nodes, Node{Name: "cp2", Roles: []string{"etcd", "controlplane"}})
	if why := RemovalBlocked(nodes, "cp1"); why != "" {
		t.Errorf("cp1 with a second control-plane node: %s", why)
	}
}

func TestAllowedByTier(t *testing.T) {
	prov := Cluster{Tier: TierProvisioned, UpgradeOwner: OwnerRancher}
	att := Cluster{Tier: TierAttached, UpgradeOwner: OwnerNone}
	host := Cluster{Tier: TierHosted, UpgradeOwner: OwnerCloud}
	for _, tc := range []struct {
		c    Cluster
		op   Operation
		want bool
	}{
		{prov, OpSnapshot, true}, {prov, OpRestore, true}, {prov, OpRemove, true}, {prov, OpRegister, true},
		{att, OpSnapshot, false}, {att, OpRestore, false}, {att, OpDelete, true}, {att, OpRegister, true}, {att, OpUpgrade, false},
		{host, OpRegister, false}, {host, OpDelete, false}, {host, OpRestore, false},
		{Cluster{Tier: TierAttached, UpgradeOwner: OwnerRancher}, OpUpgrade, false},
	} {
		if ok, why := tc.c.Allowed(tc.op); ok != tc.want || (!ok && why == "") {
			t.Errorf("%s %s: allowed=%v (%q), want %v", tc.c.Tier, tc.op, ok, why, tc.want)
		}
	}
}

func TestAllowedUpgradeByOwner(t *testing.T) {
	// Rule 5: only an owner KubeStack can act through may be upgraded; unknown or empty owners fail closed.
	for owner, want := range map[UpgradeOwner]bool{OwnerRancher: true, OwnerKubeStack: false, OwnerCloud: false, OwnerNone: false, "": false} {
		if ok, why := (Cluster{Tier: TierProvisioned, UpgradeOwner: owner}).Allowed(OpUpgrade); ok != want || (!ok && why == "") {
			t.Errorf("owner %q: allowed=%v (%q), want %v", owner, ok, why, want)
		}
	}
	prov := Cluster{Tier: TierProvisioned, UpgradeOwner: OwnerRancher}
	if ok, _ := prov.Allowed(OpAccess); !ok {
		t.Error("provisioned access refused")
	}
	if ok, _ := prov.Allowed("bogus"); ok {
		t.Error("unknown operation allowed")
	}
	if ok, _ := (Cluster{Tier: "", UpgradeOwner: OwnerRancher}).Allowed(OpDelete); ok {
		t.Error("cluster with no tier allowed delete")
	}
}

func TestValidate(t *testing.T) {
	versions := []string{"v1.31.4+rke2r1"}
	for _, tc := range []struct {
		r  CreateRequest
		ok bool
	}{
		{CreateRequest{Name: "prod-1", KubernetesVersion: versions[0]}, true},
		{CreateRequest{Name: "Prod", KubernetesVersion: versions[0]}, false},
		{CreateRequest{Name: "-x", KubernetesVersion: versions[0]}, false},
		{CreateRequest{Name: "", KubernetesVersion: versions[0]}, false},
		{CreateRequest{Name: string(make([]byte, 64)), KubernetesVersion: versions[0]}, false},
		{CreateRequest{Name: "prod", KubernetesVersion: "v1.99.0"}, false},
	} {
		if err := tc.r.Validate(versions); (err == nil) != tc.ok {
			t.Errorf("%+v: err=%v want ok=%v", tc.r, err, tc.ok)
		}
	}
	if (ImportRequest{Name: "edge"}).Validate() != nil || (ImportRequest{Name: "a_b"}).Validate() == nil {
		t.Error("ImportRequest name validation")
	}
}
