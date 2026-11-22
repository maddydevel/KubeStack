// Copyright 2026 The KubeStack Authors. All rights reserved.

// Command kubestack-controller reconciles KubeStack CRDs (api/v1alpha1) to the lifecycle engine.
package main

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"kubestack.dev/kubestack/api/v1alpha1"
	"kubestack.dev/kubestack/internal/audit"
	"kubestack.dev/kubestack/internal/auth"
	"kubestack.dev/kubestack/internal/controller"
	"kubestack.dev/kubestack/internal/engine/rancher"
	"kubestack.dev/kubestack/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctrl.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))
	ctx := ctrl.SetupSignalHandler()

	// The CRDs live on the engine's local cluster. Empty path = in-cluster service account; never logged.
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBESTACK_RANCHER_KUBECONFIG"))
	if err != nil {
		return err
	}
	eng, err := rancher.New(cfg)
	if err != nil {
		return err
	}

	var st audit.Sink
	if url := os.Getenv("KUBESTACK_DATABASE_URL"); url != "" {
		pg, err := store.NewPostgres(ctx, url)
		if err != nil {
			return errors.New("connect to database failed") // the DSN may embed a password; don't wrap it
		}
		st = pg
	} else {
		slog.Warn("KUBESTACK_DATABASE_URL unset: audit trail is in memory and lost on restart")
		st = store.NewMemory()
	}

	adminGroup := os.Getenv("KUBESTACK_ADMIN_GROUP")
	if adminGroup == "" {
		adminGroup = "kubestack-admins"
	}
	oidc, err := auth.ClusterOIDCFromEnv(adminGroup)
	if err != nil {
		return err
	}
	if oidc == nil {
		slog.Warn("KUBESTACK_OIDC_ISSUER unset: new clusters will not trust an IdP for direct access")
	}

	sch := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(sch); err != nil {
		return err
	}
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 sch,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: ":8081",
		LeaderElection:         true,
		LeaderElectionID:       "kubestack-controller",
		// In-cluster the namespace is detected; out of cluster set it explicitly.
		LeaderElectionNamespace: os.Getenv("KUBESTACK_NAMESPACE"),
	})
	if err != nil {
		return err
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	r := &controller.ClusterReconciler{Client: mgr.GetClient(), Engine: eng, Audit: st, OIDC: oidc, Now: time.Now}
	if err := r.SetupWithManager(mgr); err != nil {
		return err
	}
	slog.Info("starting controller")
	return mgr.Start(ctx)
}
