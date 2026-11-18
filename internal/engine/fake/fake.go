// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package fake is an in-memory engine for tests and local development.
package fake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
)

type Cluster struct {
	domain.Cluster
	Snapshots []domain.Snapshot
	Nodes     []domain.Node
}

func nodes(ready, total int) []domain.Node {
	out := []domain.Node{}
	for i := range total {
		roles := []string{"worker"}
		if i == 0 {
			roles = []string{"etcd", "controlplane"}
		}
		out = append(out, domain.Node{Name: fmt.Sprintf("node-%d", i), Ready: i < ready, Roles: roles})
	}
	return out
}

type Engine struct {
	mu       sync.Mutex
	clusters map[string]*Cluster
}

var _ engine.Engine = (*Engine)(nil)

func New(cs ...Cluster) *Engine {
	e := &Engine{clusters: map[string]*Cluster{}}
	for _, c := range cs {
		e.clusters[c.ID] = &c
	}
	return e
}

// Demo returns a fake seeded with one cluster per tier.
func Demo() *Engine {
	now := time.Now()
	return New(
		Cluster{domain.Cluster{ID: "prod-eu", Name: "prod-eu", Tier: domain.TierProvisioned, UpgradeOwner: domain.OwnerRancher, KubernetesVersion: "v1.34.12+rke2r1", Ready: true},
			[]domain.Snapshot{{Name: "prod-eu-etcd-1", CreatedAt: now.Add(-2 * time.Hour), Successful: true}}, nodes(3, 3)},
		Cluster{domain.Cluster{ID: "analytics-eks", Name: "analytics-eks", Tier: domain.TierHosted, UpgradeOwner: domain.OwnerCloud, KubernetesVersion: "v1.33.4-eks", Ready: true}, nil, nodes(4, 4)},
		Cluster{domain.Cluster{ID: "legacy-k3s", Name: "legacy-k3s", Tier: domain.TierAttached, UpgradeOwner: domain.OwnerNone, KubernetesVersion: "v1.31.6+k3s1", Ready: true}, nil, nodes(1, 1)},
	)
}

func (e *Engine) ListClusters(context.Context) ([]domain.Cluster, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]domain.Cluster, 0, len(e.clusters))
	for _, c := range e.clusters {
		out = append(out, c.Cluster)
	}
	slices.SortFunc(out, func(a, b domain.Cluster) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (e *Engine) GetCluster(_ context.Context, id string) (domain.Cluster, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.clusters[id]
	if !ok {
		return domain.Cluster{}, engine.ErrNotFound
	}
	return c.Cluster, nil
}

func (e *Engine) CreateCluster(_ context.Context, r domain.CreateRequest) (domain.Cluster, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.clusters[r.Name]; ok {
		return domain.Cluster{}, engine.ErrConflict
	}
	c := &Cluster{Cluster: domain.Cluster{ID: r.Name, Name: r.Name, Tier: domain.TierProvisioned, UpgradeOwner: domain.OwnerRancher, KubernetesVersion: r.KubernetesVersion}}
	e.clusters[c.ID] = c
	return c.Cluster, nil
}

func (e *Engine) ImportCluster(_ context.Context, r domain.ImportRequest) (domain.Cluster, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.clusters[r.Name]; ok {
		return domain.Cluster{}, engine.ErrConflict
	}
	c := &Cluster{Cluster: domain.Cluster{ID: r.Name, Name: r.Name, Tier: domain.TierAttached, UpgradeOwner: domain.OwnerNone}}
	e.clusters[c.ID] = c
	return c.Cluster, nil
}

func (e *Engine) Registration(_ context.Context, id string) (domain.Registration, error) {
	c, err := e.GetCluster(context.Background(), id)
	if err != nil {
		return domain.Registration{}, err
	}
	cmd := "curl -fL https://engine.example/install.sh | sudo sh -s - --token " + domain.TokenPlaceholder
	if c.Tier == domain.TierAttached {
		cmd = "kubectl apply -f https://engine.example/import/" + domain.TokenPlaceholder + "_" + id + ".yaml"
	}
	return domain.Registration{Command: cmd, Token: domain.SecretRef{Namespace: "kubestack-demo", Name: "join-" + id, Key: "token"}}, nil
}

func (e *Engine) UpgradeCluster(_ context.Context, id, v string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.clusters[id]
	if !ok {
		return engine.ErrNotFound
	}
	c.KubernetesVersion = v
	return nil
}

func (e *Engine) DeleteCluster(_ context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.clusters[id]; !ok {
		return engine.ErrNotFound
	}
	delete(e.clusters, id)
	return nil
}

func (e *Engine) get(id string) (*Cluster, error) {
	c, ok := e.clusters[id]
	if !ok {
		return nil, engine.ErrNotFound
	}
	return c, nil
}

func (e *Engine) Snapshots(_ context.Context, id string) ([]domain.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.get(id)
	if err != nil {
		return nil, err
	}
	return slices.Clone(c.Snapshots), nil
}

func (e *Engine) CreateSnapshot(_ context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.get(id)
	if err != nil {
		return err
	}
	c.Snapshots = append(c.Snapshots, domain.Snapshot{Name: fmt.Sprintf("%s-etcd-%d", id, len(c.Snapshots)+1), CreatedAt: time.Now(), Successful: true})
	return nil
}

func (e *Engine) RestoreSnapshot(_ context.Context, id, snapshot string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.get(id)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(c.Snapshots, func(s domain.Snapshot) bool { return s.Name == snapshot }) {
		return engine.ErrNotFound
	}
	return nil
}

func (e *Engine) Nodes(_ context.Context, id string) ([]domain.Node, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.get(id)
	if err != nil {
		return nil, err
	}
	return slices.Clone(c.Nodes), nil
}

func (e *Engine) Access(_ context.Context, id string) (domain.Access, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.get(id); err != nil {
		return domain.Access{}, err
	}
	return domain.Access{Servers: []string{"https://" + id + ".example.internal:6443"}}, nil
}

func (e *Engine) RemoveNode(_ context.Context, id, node string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.get(id)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(c.Nodes, func(n domain.Node) bool { return n.Name == node })
	if i < 0 {
		return engine.ErrNotFound
	}
	c.Nodes = slices.Delete(c.Nodes, i, i+1)
	return nil
}
