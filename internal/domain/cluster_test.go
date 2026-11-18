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
