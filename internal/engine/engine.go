// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package engine defines the lifecycle engine boundary. Implementations: rancher/, fake/.
package engine

import (
	"context"
	"errors"
	"time"

	"kubestack.dev/kubestack/internal/domain"
)

var (
	ErrNotFound = errors.New("cluster not found")
	ErrConflict = errors.New("cluster already exists")
	ErrNotReady = errors.New("not ready yet")
)

type Engine interface {
	ListClusters(ctx context.Context) ([]domain.Cluster, error)
	GetCluster(ctx context.Context, id string) (domain.Cluster, error)
	CreateCluster(ctx context.Context, req domain.CreateRequest) (domain.Cluster, error)
	ImportCluster(ctx context.Context, req domain.ImportRequest) (domain.Cluster, error)
	// Registration returns ErrNotReady until the engine has issued a join token for the cluster.
	Registration(ctx context.Context, id string) (domain.Registration, error)
	UpgradeCluster(ctx context.Context, id, kubernetesVersion string) error
	DeleteCluster(ctx context.Context, id string) error
	// LatestSnapshot returns the newest successful etcd snapshot time, zero if none.
	LatestSnapshot(ctx context.Context, id string) (time.Time, error)
	NodeHealth(ctx context.Context, id string) (ready, total int, err error)
}
