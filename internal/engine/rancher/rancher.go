// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package rancher is the ONLY package that knows Rancher. It talks to Rancher's local
// cluster exclusively through native Kubernetes CRD APIs (no Steve /v1, no Norman /v3).
package rancher

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
)

var (
	provGVR    = schema.GroupVersionResource{Group: "provisioning.cattle.io", Version: "v1", Resource: "clusters"}
	mgmtGVR    = schema.GroupVersionResource{Group: "management.cattle.io", Version: "v3", Resource: "clusters"}
	snapGVR    = schema.GroupVersionResource{Group: "rke.cattle.io", Version: "v1", Resource: "etcdsnapshots"}
	machineGVR = schema.GroupVersionResource{Group: "cluster.x-k8s.io", Version: "v1beta2", Resource: "machines"}
	crtGVR     = schema.GroupVersionResource{Group: "management.cattle.io", Version: "v3", Resource: "clusterregistrationtokens"}
)

// Downstream clusters live in fleet-default; fleet-local holds Rancher's own "local" cluster,
// which is an implementation detail and never shown as a product cluster.
const namespace = "fleet-default"

type Engine struct{ c dynamic.Interface }

var _ engine.Engine = (*Engine)(nil)

func New(cfg *rest.Config) (*Engine, error) {
	c, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Engine{c: c}, nil
}

func NewForClient(c dynamic.Interface) *Engine { return &Engine{c: c} }

func (e *Engine) prov() dynamic.ResourceInterface { return e.c.Resource(provGVR).Namespace(namespace) }

func mapErr(err error) error {
	switch {
	case apierrors.IsNotFound(err):
		return engine.ErrNotFound
	case apierrors.IsAlreadyExists(err):
		return engine.ErrConflict
	}
	return err
}

// toDomain classifies a provisioning cluster. mgmt is its management.cattle.io cluster, may be nil.
func toDomain(p, mgmt *unstructured.Unstructured) domain.Cluster {
	ver, _, _ := unstructured.NestedString(p.Object, "spec", "kubernetesVersion")
	ready, _, _ := unstructured.NestedBool(p.Object, "status", "ready")
	c := domain.Cluster{ID: p.GetName(), Name: p.GetName(), KubernetesVersion: ver, Ready: ready,
		Tier: domain.TierAttached, UpgradeOwner: domain.OwnerNone}

	// spec.rkeConfig present => RKE2/K3s cluster provisioned by the engine, which owns the version.
	if _, ok, _ := unstructured.NestedMap(p.Object, "spec", "rkeConfig"); ok {
		c.Tier, c.UpgradeOwner = domain.TierProvisioned, domain.OwnerRancher
		return c
	}
	if mgmt == nil {
		return c
	}
	if c.KubernetesVersion == "" {
		c.KubernetesVersion, _, _ = unstructured.NestedString(mgmt.Object, "status", "version", "gitVersion")
	}
	for _, k := range []string{"eksConfig", "aksConfig", "gkeConfig"} {
		if v, ok, _ := unstructured.NestedFieldNoCopy(mgmt.Object, "spec", k); ok && v != nil {
			c.Tier, c.UpgradeOwner = domain.TierHosted, domain.OwnerCloud
			return c
		}
	}
	// Imported clusters stay upgradeOwner=none (conservative: refuses upgrades) until we
	// verify per-version semantics of mgmt spec.rke2Config/k3sConfig.
	return c
}

func (e *Engine) ListClusters(ctx context.Context) ([]domain.Cluster, error) {
	ps, err := e.prov().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	ms, err := e.c.Resource(mgmtGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	mgmt := map[string]*unstructured.Unstructured{}
	for i := range ms.Items {
		mgmt[ms.Items[i].GetName()] = &ms.Items[i]
	}
	out := make([]domain.Cluster, 0, len(ps.Items))
	for i := range ps.Items {
		id, _, _ := unstructured.NestedString(ps.Items[i].Object, "status", "clusterName")
		out = append(out, toDomain(&ps.Items[i], mgmt[id]))
	}
	return out, nil
}

func (e *Engine) GetCluster(ctx context.Context, id string) (domain.Cluster, error) {
	p, err := e.prov().Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		return domain.Cluster{}, mapErr(err)
	}
	var mgmt *unstructured.Unstructured
	if mid, _, _ := unstructured.NestedString(p.Object, "status", "clusterName"); mid != "" {
		mgmt, err = e.c.Resource(mgmtGVR).Get(ctx, mid, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return domain.Cluster{}, err
		}
		if apierrors.IsNotFound(err) {
			mgmt = nil
		}
	}
	return toDomain(p, mgmt), nil
}

func (e *Engine) CreateCluster(ctx context.Context, r domain.CreateRequest) (domain.Cluster, error) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "provisioning.cattle.io/v1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": r.Name, "namespace": namespace},
		"spec": map[string]any{
			"kubernetesVersion": r.KubernetesVersion,
			"rkeConfig": map[string]any{
				// Traefik is the default ingress (rule 7). RKE2 option documented at
				// https://docs.rke2.io/reference/server_config ("ingress-controller").
				"machineGlobalConfig": map[string]any{"ingress-controller": "traefik"},
			},
		},
	}}
	created, err := e.prov().Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return domain.Cluster{}, mapErr(err)
	}
	return toDomain(created, nil), nil
}

// ImportCluster creates a provisioning cluster without rkeConfig: Rancher then issues an import
// manifest for the existing cluster's agent.
func (e *Engine) ImportCluster(ctx context.Context, r domain.ImportRequest) (domain.Cluster, error) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "provisioning.cattle.io/v1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": r.Name, "namespace": namespace},
		"spec":       map[string]any{},
	}}
	created, err := e.prov().Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return domain.Cluster{}, mapErr(err)
	}
	return toDomain(created, nil), nil
}

// Registration reads the cluster's ClusterRegistrationToken. Since Rancher v2.15 its status commands
// carry a "{token}" placeholder and the token lives in the Secret named by status.tokenSecretName
// (key "token") in the management cluster's namespace — see Rancher v2.15.3
// pkg/controllers/dashboard/clusterregistrationtoken/{status,token_secret}.go. status.token is never read.
func (e *Engine) Registration(ctx context.Context, id string) (domain.Registration, error) {
	p, err := e.prov().Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		return domain.Registration{}, mapErr(err)
	}
	mid, _, _ := unstructured.NestedString(p.Object, "status", "clusterName")
	if mid == "" {
		return domain.Registration{}, fmt.Errorf("%w: cluster not linked yet", engine.ErrNotReady)
	}
	field := "command" // kubectl apply of the agent manifest, for imported clusters
	if _, ok, _ := unstructured.NestedMap(p.Object, "spec", "rkeConfig"); ok {
		field = "nodeCommand" // system-agent install on each new node
	}
	l, err := e.c.Resource(crtGVR).Namespace(mid).List(ctx, metav1.ListOptions{})
	if err != nil {
		return domain.Registration{}, err
	}
	for _, t := range l.Items {
		secret, _, _ := unstructured.NestedString(t.Object, "status", "tokenSecretName")
		cmd, _, _ := unstructured.NestedString(t.Object, "status", field)
		if secret == "" || cmd == "" {
			continue
		}
		if !strings.Contains(cmd, domain.TokenPlaceholder) {
			// An engine that inlines the token would leak it through our API (rule 15).
			return domain.Registration{}, fmt.Errorf("registration command embeds a token; refusing to return it")
		}
		return domain.Registration{Command: cmd, Token: domain.SecretRef{Namespace: mid, Name: secret, Key: "token"}}, nil
	}
	// No command until Rancher's server-url setting is set.
	return domain.Registration{}, fmt.Errorf("%w: no registration command issued (is the engine's server URL configured?)", engine.ErrNotReady)
}

func (e *Engine) UpgradeCluster(ctx context.Context, id, v string) error {
	patch := fmt.Appendf(nil, `{"spec":{"kubernetesVersion":%q}}`, v)
	_, err := e.prov().Patch(ctx, id, types.MergePatchType, patch, metav1.PatchOptions{})
	return mapErr(err)
}

func (e *Engine) DeleteCluster(ctx context.Context, id string) error {
	return mapErr(e.prov().Delete(ctx, id, metav1.DeleteOptions{}))
}

func (e *Engine) LatestSnapshot(ctx context.Context, id string) (time.Time, error) {
	l, err := e.c.Resource(snapGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "rke.cattle.io/cluster-name=" + id})
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, s := range l.Items {
		// rke.cattle.io/v1 ETCDSnapshot.snapshotFile.{status,createdAt}
		if st, _, _ := unstructured.NestedString(s.Object, "snapshotFile", "status"); st != "successful" {
			continue
		}
		raw, _, _ := unstructured.NestedString(s.Object, "snapshotFile", "createdAt")
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t = s.GetCreationTimestamp().Time
		}
		if t.After(latest) {
			latest = t
		}
	}
	return latest, nil
}

// NodeHealth counts Cluster API machines with condition Ready=True.
func (e *Engine) NodeHealth(ctx context.Context, id string) (int, int, error) {
	l, err := e.c.Resource(machineGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "cluster.x-k8s.io/cluster-name=" + id})
	if err != nil {
		return 0, 0, err
	}
	ready := 0
	for _, m := range l.Items {
		conds, _, _ := unstructured.NestedSlice(m.Object, "status", "conditions")
		for _, c := range conds {
			if cm, ok := c.(map[string]any); ok && cm["type"] == "Ready" && cm["status"] == "True" {
				ready++
				break
			}
		}
	}
	return ready, len(l.Items), nil
}
