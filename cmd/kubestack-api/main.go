// Copyright 2026 The KubeStack Authors. All rights reserved.

// Command kubestack-api serves the KubeStack API and portal.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"kubestack.dev/kubestack/internal/auth"
	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
	"kubestack.dev/kubestack/internal/engine/fake"
	"kubestack.dev/kubestack/internal/engine/rancher"
	"kubestack.dev/kubestack/internal/server"
	"kubestack.dev/kubestack/internal/store"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	adminGroup := env("KUBESTACK_ADMIN_GROUP", "kubestack-admins")

	var eng engine.Engine
	switch e := env("KUBESTACK_ENGINE", "rancher"); e {
	case "fake":
		slog.Warn("using in-memory fake engine")
		eng = fake.Demo()
	case "rancher":
		// Empty path = in-cluster service account. The kubeconfig file is never logged.
		cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBESTACK_RANCHER_KUBECONFIG"))
		if err != nil {
			return err
		}
		if eng, err = rancher.New(cfg); err != nil {
			return err
		}
	default:
		return errors.New("KUBESTACK_ENGINE must be rancher or fake, got " + e)
	}

	var st store.Store
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

	var a auth.Authenticator
	var login *auth.Login
	var clusterOIDC *domain.ClusterOIDC
	if os.Getenv("KUBESTACK_DEV_AUTH") == "true" {
		slog.Warn("DEV AUTH ENABLED: every request is an admin")
		a = auth.Dev{Group: adminGroup}
	} else {
		issuer, client := os.Getenv("KUBESTACK_OIDC_ISSUER"), os.Getenv("KUBESTACK_OIDC_CLIENT_ID")
		if issuer == "" || client == "" {
			return errors.New("KUBESTACK_OIDC_ISSUER and KUBESTACK_OIDC_CLIENT_ID are required (or KUBESTACK_DEV_AUTH=true)")
		}
		groupsClaim := env("KUBESTACK_OIDC_GROUPS_CLAIM", "groups")
		// Clusters trust the same IdP; a CLI needs a public client, so it may differ from the portal's.
		clusterOIDC = &domain.ClusterOIDC{IssuerURL: issuer, ClientID: env("KUBESTACK_OIDC_CLI_CLIENT_ID", client), GroupsClaim: groupsClaim, AdminGroup: adminGroup}
		o, err := auth.NewOIDC(ctx, issuer, client, groupsClaim)
		if err != nil {
			return err
		}
		a = o
		// Optional built-in portal login; without it, front the portal with an OIDC-aware proxy.
		if redirect := os.Getenv("KUBESTACK_OIDC_REDIRECT_URL"); redirect != "" {
			login = o.NewLogin(os.Getenv("KUBESTACK_OIDC_CLIENT_SECRET"), redirect)
		}
	}

	s := &server.Server{Engine: eng, Store: st, Auth: a, AdminGroup: adminGroup, Now: time.Now, ClusterOIDC: clusterOIDC}
	if login != nil {
		s.Login = login
	}
	srv := &http.Server{Addr: env("KUBESTACK_LISTEN", ":8080"), Handler: s.Routes(os.Getenv("KUBESTACK_PORTAL_DIR")), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			slog.Warn("shutdown", "err", err)
		}
	}()
	slog.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
