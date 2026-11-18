// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package domain holds the engine-neutral KubeStack model.
package domain

import (
	"fmt"
	"regexp"
)

type Tier string

const (
	TierProvisioned Tier = "provisioned"
	TierHosted      Tier = "hosted" // EKS/AKS/GKE
	TierAttached    Tier = "attached"
)

type UpgradeOwner string

const (
	OwnerKubeStack UpgradeOwner = "kubestack"
	OwnerRancher   UpgradeOwner = "rancher" // the lifecycle engine drives the version
	OwnerCloud     UpgradeOwner = "cloud"
	OwnerNone      UpgradeOwner = "none"
)

type Operation string

const (
	OpUpgrade Operation = "upgrade"
	OpDelete  Operation = "delete"
)

var Operations = []Operation{OpUpgrade, OpDelete}

type Cluster struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Tier              Tier         `json:"tier"`
	UpgradeOwner      UpgradeOwner `json:"upgradeOwner"`
	KubernetesVersion string       `json:"kubernetesVersion"`
	Ready             bool         `json:"ready"`
}

// Allowed is the single capability gate: the API enforces it and the UI shows its reason.
func (c Cluster) Allowed(op Operation) (bool, string) {
	if c.Tier != TierProvisioned {
		return false, fmt.Sprintf("%s clusters do not support %s from KubeStack", c.Tier, op)
	}
	switch op {
	case OpDelete:
		return true, ""
	case OpUpgrade:
		switch c.UpgradeOwner {
		case OwnerRancher:
			return true, ""
		case OwnerKubeStack:
			return false, "KubeStack-driven upgrades (System Upgrade Controller) are not implemented yet"
		default:
			return false, fmt.Sprintf("Kubernetes version is managed by %q", c.UpgradeOwner)
		}
	}
	return false, "unknown operation"
}

type CreateRequest struct {
	Name              string `json:"name"`
	KubernetesVersion string `json:"kubernetesVersion"`
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

func (r CreateRequest) Validate(versions []string) error {
	if !dnsLabel.MatchString(r.Name) {
		return fmt.Errorf("name must be a DNS-1123 label (lowercase alphanumerics and '-', max 63)")
	}
	for _, v := range versions {
		if v == r.KubernetesVersion {
			return nil
		}
	}
	return fmt.Errorf("kubernetesVersion %q is not in the release catalog", r.KubernetesVersion)
}
