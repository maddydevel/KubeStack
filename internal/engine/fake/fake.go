// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package fake is an in-memory engine for tests and local development.
package fake

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
)

type Cluster struct {
	domain.Cluster
	Snapshot   time.Time
	ReadyNodes int
	TotalNodes int
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
		Cluster{domain.Cluster{ID: "prod-eu", Name: "prod-eu", Tier: domain.TierProvisioned, UpgradeOwner: domain.OwnerRancher, KubernetesVersion: "v1.34.12+rke2r1", Ready: true}, now.Add(-2 * time.Hour), 3, 3},
		Cluster{domain.Cluster{ID: "analytics-eks", Name: "analytics-eks", Tier: domain.TierHosted, UpgradeOwner: domain.OwnerCloud, KubernetesVersion: "v1.33.4-eks", Ready: true}, time.Time{}, 4, 4},
		Cluster{domain.Cluster{ID: "legacy-k3s", Name: "legacy-k3s", Tier: domain.TierAttached, UpgradeOwner: domain.OwnerNone, KubernetesVersion: "v1.31.6+k3s1", Ready: true}, time.Time{}, 1, 1},
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

func (e *Engine) LatestSnapshot(_ context.Context, id string) (time.Time, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.clusters[id]
	if !ok {
		return time.Time{}, engine.ErrNotFound
	}
	return c.Snapshot, nil
}

func (e *Engine) NodeHealth(_ context.Context, id string) (int, int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.clusters[id]
	if !ok {
		return 0, 0, engine.ErrNotFound
	}
	return c.ReadyNodes, c.TotalNodes, nil
}
