// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package domain holds the engine-neutral KubeStack model.
package domain

import (
	"fmt"
	"regexp"
	"time"
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
	OpUpgrade  Operation = "upgrade"
	OpDelete   Operation = "delete"
	OpRegister Operation = "register" // join nodes (provisioned) or connect the cluster agent (attached)
	OpSnapshot Operation = "snapshot" // take an etcd snapshot
	OpRestore  Operation = "restore"  // restore etcd from a snapshot (destructive)
	OpRemove   Operation = "removeNode"
	OpAccess   Operation = "access" // direct API access with the user's own identity
)

var Operations = []Operation{OpUpgrade, OpDelete, OpRegister, OpSnapshot, OpRestore, OpRemove, OpAccess}

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
	if op == OpRegister {
		if c.Tier == TierHosted {
			return false, "hosted clusters' nodes are managed by the cloud provider"
		}
		return true, ""
	}
	// Deleting an attached cluster only disconnects it (the engine removes its agent); the cluster survives.
	if op == OpDelete && c.Tier == TierAttached {
		return true, ""
	}
	if op == OpAccess && c.Tier != TierProvisioned {
		return false, "KubeStack configures identity only on clusters it provisions; use the cluster's own access setup"
	}
	if c.Tier != TierProvisioned {
		return false, fmt.Sprintf("%s clusters do not support %s from KubeStack", c.Tier, op)
	}
	switch op {
	case OpDelete, OpSnapshot, OpRestore, OpRemove, OpAccess:
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
	// OIDC is set by the server from its own configuration, never from the request body.
	OIDC *ClusterOIDC `json:"-"`
}

// ClusterOIDC makes a cluster's API server trust the organisation's IdP so people use their own
// identity downstream (rule 16). Usernames and groups get the "oidc:" prefix inside the cluster.
type ClusterOIDC struct {
	IssuerURL   string `json:"issuerURL"`
	ClientID    string `json:"clientID"`
	GroupsClaim string `json:"-"`
	AdminGroup  string `json:"-"` // bound to cluster-admin as "oidc:<group>"
}

const OIDCPrefix = "oidc:"

// Access is how a person reaches a cluster's API directly with their own IdP token. No credentials.
type Access struct {
	Servers                  []string     `json:"servers"`
	CertificateAuthorityData string       `json:"certificateAuthorityData"` // base64 PEM, may be empty
	OIDC                     *ClusterOIDC `json:"oidc"`
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

func validName(n string) error {
	if !dnsLabel.MatchString(n) {
		return fmt.Errorf("name must be a DNS-1123 label (lowercase alphanumerics and '-', max 63)")
	}
	return nil
}

func (r CreateRequest) Validate(versions []string) error {
	if err := validName(r.Name); err != nil {
		return err
	}
	for _, v := range versions {
		if v == r.KubernetesVersion {
			return nil
		}
	}
	return fmt.Errorf("kubernetesVersion %q is not in the release catalog", r.KubernetesVersion)
}

// ImportRequest attaches an existing CNCF-conformant cluster.
type ImportRequest struct {
	Name string `json:"name"`
}

func (r ImportRequest) Validate() error { return validName(r.Name) }

// TokenPlaceholder appears literally in Registration.Command where the join token goes.
const TokenPlaceholder = "{token}"

// Registration tells an operator how to join nodes (provisioned) or connect an existing cluster
// (attached). The token itself never passes through KubeStack (rule 15): it stays in the referenced
// Kubernetes Secret, which the operator reads with their own credentials.
type Registration struct {
	Command string    `json:"command"`
	Token   SecretRef `json:"token"`
}

type SecretRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Key       string `json:"key"`
}

type Snapshot struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	Successful bool      `json:"successful"`
}

type Node struct {
	Name  string   `json:"name"`
	Ready bool     `json:"ready"`
	Roles []string `json:"roles"` // etcd, controlplane, worker
}

// RemovalBlocked refuses removing a node that would leave the cluster without etcd or a control plane.
func RemovalBlocked(nodes []Node, name string) string {
	var target *Node
	count := map[string]int{}
	for i, n := range nodes {
		if n.Name == name {
			target = &nodes[i]
		}
		for _, r := range n.Roles {
			count[r]++
		}
	}
	if target == nil {
		return "node not found in this cluster"
	}
	for _, r := range target.Roles {
		if (r == "etcd" || r == "controlplane") && count[r] == 1 {
			return "refusing to remove the last " + r + " node"
		}
	}
	return ""
}
