// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package rancher is the ONLY package that knows Rancher. It talks to Rancher's local
// cluster exclusively through native Kubernetes CRD APIs (no Steve /v1, no Norman /v3).
package rancher

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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

// Rancher v2.15.3 pkg/controllers/management/importedclusterversionmanagement: Rancher upgrades an
// imported RKE2/K3s cluster when this annotation is "true", or when it is absent/"system-default" and
// the setting of the same name is "true".
const (
	versionMgmtAnno    = "rancher.io/imported-cluster-version-management"
	versionMgmtSetting = "imported-cluster-version-management"
)

var settingGVR = schema.GroupVersionResource{Group: "management.cattle.io", Version: "v3", Resource: "settings"}

// importedVersionManaged reads the global default; on any error it reports false (conservative).
func (e *Engine) importedVersionManaged(ctx context.Context) bool {
	s, err := e.c.Resource(settingGVR).Get(ctx, versionMgmtSetting, metav1.GetOptions{})
	if err != nil {
		return false
	}
	v, _, _ := unstructured.NestedString(s.Object, "value")
	if v == "" {
		v, _, _ = unstructured.NestedString(s.Object, "default")
	}
	return v == "true"
}

// toDomain classifies a provisioning cluster. mgmt is its management.cattle.io cluster, may be nil.
// versionMgmtDefault is the imported-cluster version management setting.
func toDomain(p, mgmt *unstructured.Unstructured, versionMgmtDefault bool) domain.Cluster {
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
	// Imported RKE2/K3s: if the engine manages their version, it owns upgrades (rule 5: KubeStack must
	// never run its own upgrade plans there). Other imported clusters stay upgradeOwner=none.
	if d, _, _ := unstructured.NestedString(mgmt.Object, "status", "driver"); d == "rke2" || d == "k3s" {
		managed := versionMgmtDefault
		switch mgmt.GetAnnotations()[versionMgmtAnno] {
		case "true":
			managed = true
		case "false":
			managed = false
		}
		if managed {
			c.UpgradeOwner = domain.OwnerRancher
		}
	}
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
	def := e.importedVersionManaged(ctx)
	out := make([]domain.Cluster, 0, len(ps.Items))
	for i := range ps.Items {
		id, _, _ := unstructured.NestedString(ps.Items[i].Object, "status", "clusterName")
		out = append(out, toDomain(&ps.Items[i], mgmt[id], def))
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
	return toDomain(p, mgmt, e.importedVersionManaged(ctx)), nil
}

func (e *Engine) CreateCluster(ctx context.Context, r domain.CreateRequest) (domain.Cluster, error) {
	// Traefik is the default ingress (rule 7). RKE2 option documented at
	// https://docs.rke2.io/reference/server_config ("ingress-controller").
	global := map[string]any{"ingress-controller": "traefik"}
	rke := map[string]any{"machineGlobalConfig": global}
	if o := r.OIDC; o != nil {
		// kube-apiserver OIDC flags (https://kubernetes.io/docs/reference/access-authn-authz/authentication/#openid-connect-tokens);
		// RKE2 passes kube-apiserver-arg through on server nodes.
		args := []any{
			"oidc-issuer-url=" + o.IssuerURL, "oidc-client-id=" + o.ClientID,
			"oidc-username-claim=email", "oidc-groups-claim=" + o.GroupsClaim,
			"oidc-username-prefix=" + domain.OIDCPrefix, "oidc-groups-prefix=" + domain.OIDCPrefix,
		}
		if len(o.CAPEM) > 0 {
			// machineSelectorFiles writes the CA onto control-plane nodes; RKE2's kube-apiserver-extra-mount
			// makes the directory visible inside the static pod (https://docs.rke2.io/reference/server_config).
			args = append(args, "oidc-ca-file="+oidcCAPath)
			global["kube-apiserver-extra-mount"] = []any{oidcCADir + ":" + oidcCADir + ":ro"}
			rke["machineSelectorFiles"] = []any{map[string]any{
				"machineLabelSelector": map[string]any{"matchLabels": map[string]any{roleLabels["controlplane"]: "true"}},
				"fileSources": []any{map[string]any{"configMap": map[string]any{
					"name": oidcCAConfigMap(r.Name), "items": []any{map[string]any{"key": "ca.crt", "path": oidcCAPath}}}}},
			}}
		}
		global["kube-apiserver-arg"] = args
		// Applied by Rancher once the cluster is up: the admin group gets cluster-admin, as itself.
		rke["additionalManifest"] = fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubestack-admins
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: cluster-admin}
subjects:
  - {apiGroup: rbac.authorization.k8s.io, kind: Group, name: %q}
`, domain.OIDCPrefix+o.AdminGroup)
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "provisioning.cattle.io/v1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": r.Name, "namespace": namespace},
		"spec":       map[string]any{"kubernetesVersion": r.KubernetesVersion, "rkeConfig": rke},
	}}
	created, err := e.prov().Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return domain.Cluster{}, mapErr(err)
	}
	if o := r.OIDC; o != nil && len(o.CAPEM) > 0 {
		if err := e.createOIDCCA(ctx, created, o.CAPEM); err != nil {
			return domain.Cluster{}, fmt.Errorf("cluster created but its IdP CA could not be stored: %w", err)
		}
	}
	return toDomain(created, nil, false), nil
}

const (
	oidcCADir  = "/etc/kubestack"
	oidcCAPath = oidcCADir + "/oidc-ca.crt"
)

func oidcCAConfigMap(cluster string) string { return "kubestack-oidc-ca-" + cluster }

var configMapGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

// createOIDCCA stores the IdP CA (public data, so a ConfigMap) owned by the cluster, so it is deleted
// with it. Rancher only copies files from objects whose rke.cattle.io/object-authorized-for-clusters
// annotation names the cluster (Rancher v2.15.3 pkg/capr/planner/config.go clusterObjectAuthorized).
func (e *Engine) createOIDCCA(ctx context.Context, cluster *unstructured.Unstructured, ca []byte) error {
	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{
			"name": oidcCAConfigMap(cluster.GetName()), "namespace": namespace,
			"annotations": map[string]any{"rke.cattle.io/object-authorized-for-clusters": cluster.GetName()},
			"ownerReferences": []any{map[string]any{
				"apiVersion": "provisioning.cattle.io/v1", "kind": "Cluster", "name": cluster.GetName(), "uid": string(cluster.GetUID())}},
		},
		"data": map[string]any{"ca.crt": string(ca)},
	}}
	_, err := e.c.Resource(configMapGVR).Namespace(namespace).Create(ctx, cm, metav1.CreateOptions{})
	return err
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
	return toDomain(created, nil, false), nil
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

// Snapshots lists rke.cattle.io/v1 ETCDSnapshots: snapshotFile.{status,createdAt}.
func (e *Engine) Snapshots(ctx context.Context, id string) ([]domain.Snapshot, error) {
	l, err := e.c.Resource(snapGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "rke.cattle.io/cluster-name=" + id})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Snapshot, 0, len(l.Items))
	for _, s := range l.Items {
		st, _, _ := unstructured.NestedString(s.Object, "snapshotFile", "status")
		raw, _, _ := unstructured.NestedString(s.Object, "snapshotFile", "createdAt")
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t = s.GetCreationTimestamp().Time
		}
		out = append(out, domain.Snapshot{Name: s.GetName(), CreatedAt: t, Successful: st == "successful"})
	}
	return out, nil
}

// bumpGeneration requests a one-shot rkeConfig operation (etcdSnapshotCreate/etcdSnapshotRestore):
// Rancher acts when the generation changes. resourceVersion makes concurrent requests conflict, not merge.
func (e *Engine) bumpGeneration(ctx context.Context, id, field string, extra map[string]any) error {
	p, err := e.prov().Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		return mapErr(err)
	}
	gen, _, _ := unstructured.NestedInt64(p.Object, "spec", "rkeConfig", field, "generation")
	op := map[string]any{"generation": gen + 1}
	for k, v := range extra {
		op[k] = v
	}
	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": p.GetResourceVersion()},
		"spec":     map[string]any{"rkeConfig": map[string]any{field: op}},
	})
	_, err = e.prov().Patch(ctx, id, types.MergePatchType, patch, metav1.PatchOptions{})
	if apierrors.IsConflict(err) {
		return fmt.Errorf("%w: cluster changed concurrently, retry", engine.ErrConflict)
	}
	return mapErr(err)
}

func (e *Engine) CreateSnapshot(ctx context.Context, id string) error {
	return e.bumpGeneration(ctx, id, "etcdSnapshotCreate", nil)
}

// RestoreSnapshot uses restoreRKEConfig "none": etcd data only, keeping the current version and config.
func (e *Engine) RestoreSnapshot(ctx context.Context, id, snapshot string) error {
	return e.bumpGeneration(ctx, id, "etcdSnapshotRestore", map[string]any{"name": snapshot, "restoreRKEConfig": "none"})
}

// Role labels from Rancher v2.15.3 pkg/capr/common.go.
var roleLabels = map[string]string{"etcd": "rke.cattle.io/etcd-role", "controlplane": "rke.cattle.io/control-plane-role", "worker": "rke.cattle.io/worker-role"}

// Nodes lists Cluster API machines; Ready is condition Ready=True.
func (e *Engine) Nodes(ctx context.Context, id string) ([]domain.Node, error) {
	l, err := e.c.Resource(machineGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "cluster.x-k8s.io/cluster-name=" + id})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Node, 0, len(l.Items))
	for _, m := range l.Items {
		n := domain.Node{Name: m.GetName(), Roles: []string{}}
		conds, _, _ := unstructured.NestedSlice(m.Object, "status", "conditions")
		for _, c := range conds {
			if cm, ok := c.(map[string]any); ok && cm["type"] == "Ready" && cm["status"] == "True" {
				n.Ready = true
			}
		}
		for _, r := range []string{"etcd", "controlplane", "worker"} {
			if m.GetLabels()[roleLabels[r]] == "true" {
				n.Roles = append(n.Roles, r)
			}
		}
		out = append(out, n)
	}
	return out, nil
}

// RemoveNode deletes the CAPI Machine; Rancher drains and removes the node. The machine must belong to id.
func (e *Engine) RemoveNode(ctx context.Context, id, node string) error {
	m, err := e.c.Resource(machineGVR).Namespace(namespace).Get(ctx, node, metav1.GetOptions{})
	if err != nil {
		return mapErr(err)
	}
	if m.GetLabels()["cluster.x-k8s.io/cluster-name"] != id {
		return engine.ErrNotFound
	}
	return mapErr(e.c.Resource(machineGVR).Namespace(namespace).Delete(ctx, node, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: ptr(m.GetUID())}}))
}

func ptr[T any](v T) *T { return &v }

// Access: servers are the control-plane machines' CAPI status.addresses (RKE2's serving cert covers node
// IPs) on 6443. The CA is the management cluster's status.caCert, which Rancher's agent tunnel copies from
// the downstream in-cluster CA (Rancher v2.15.3 pkg/tunnelserver/mcmauthorizer/tunnel.go). status.apiEndpoint
// is the in-cluster service address and is not reachable by people, so it is not used.
func (e *Engine) Access(ctx context.Context, id string) (domain.Access, error) {
	p, err := e.prov().Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		return domain.Access{}, mapErr(err)
	}
	l, err := e.c.Resource(machineGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "cluster.x-k8s.io/cluster-name=" + id + "," + roleLabels["controlplane"] + "=true"})
	if err != nil {
		return domain.Access{}, err
	}
	var a domain.Access
	for _, m := range l.Items {
		addrs, _, _ := unstructured.NestedSlice(m.Object, "status", "addresses")
		best := ""
		for _, x := range addrs {
			am, _ := x.(map[string]any)
			switch addr, _ := am["address"].(string); am["type"] {
			case "ExternalIP":
				best = addr
			case "InternalIP":
				if best == "" {
					best = addr
				}
			}
		}
		if best != "" {
			a.Servers = append(a.Servers, "https://"+net.JoinHostPort(best, "6443"))
		}
	}
	if len(a.Servers) == 0 {
		return domain.Access{}, fmt.Errorf("%w: no control-plane node has an address yet", engine.ErrNotReady)
	}
	if mid, _, _ := unstructured.NestedString(p.Object, "status", "clusterName"); mid != "" {
		if m, err := e.c.Resource(mgmtGVR).Get(ctx, mid, metav1.GetOptions{}); err == nil {
			a.CertificateAuthorityData, _, _ = unstructured.NestedString(m.Object, "status", "caCert")
		}
	}
	return a, nil
}
