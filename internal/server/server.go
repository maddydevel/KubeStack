// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package server is the KubeStack HTTP API (see api/openapi.yaml).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"kubestack.dev/kubestack/internal/audit"
	"kubestack.dev/kubestack/internal/auth"
	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
	"kubestack.dev/kubestack/internal/store"
	"kubestack.dev/kubestack/internal/upgrade"
)

type Server struct {
	Engine     engine.Engine
	Store      store.Store
	Auth       auth.Authenticator
	AdminGroup string // required for every mutating operation
	Now        func() time.Time
	Login      interface{ Routes(*http.ServeMux) } // optional portal login flow
}

type ctxKey struct{}

// reply is what every handler returns; the wrapper serialises it (and stores it for idempotency).
type reply struct {
	status int
	body   any
}

type apiError struct {
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

func fail(status int, msg string, details ...string) reply {
	return reply{status, apiError{Error: msg, Details: details}}
}

type handler func(r *http.Request, u auth.User) reply

func (s *Server) Routes(portalDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	if s.Login != nil {
		s.Login.Routes(mux)
	}
	mux.Handle("GET /api/v1/me", s.wrap(false, false, s.me))
	mux.Handle("GET /api/v1/versions", s.wrap(false, false, s.versions))
	mux.Handle("GET /api/v1/clusters", s.wrap(false, false, s.listClusters))
	mux.Handle("GET /api/v1/clusters/{id}", s.wrap(false, false, s.getCluster))
	mux.Handle("POST /api/v1/clusters", s.wrap(true, false, s.createCluster))
	mux.Handle("POST /api/v1/clusters/import", s.wrap(true, false, s.importCluster))
	mux.Handle("GET /api/v1/clusters/{id}/registration", s.wrap(true, false, s.registration))
	mux.Handle("POST /api/v1/clusters/{id}/upgrade", s.wrap(true, true, s.upgradeCluster))
	mux.Handle("DELETE /api/v1/clusters/{id}", s.wrap(true, true, s.deleteCluster))
	mux.Handle("GET /api/v1/audit", s.wrap(false, false, s.listAudit))
	if portalDir != "" {
		mux.Handle("GET /", http.FileServer(http.Dir(portalDir)))
	}
	return mux
}

func write(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

func (s *Server) wrap(mutating, idempotent bool, h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Auth.Authenticate(r)
		if err != nil {
			b, _ := json.Marshal(apiError{Error: "unauthenticated"})
			write(w, http.StatusUnauthorized, b)
			return
		}
		run := func() (int, []byte) {
			if mutating && !u.InGroup(s.AdminGroup) {
				b, _ := json.Marshal(apiError{Error: "forbidden: requires group " + s.AdminGroup})
				return http.StatusForbidden, b
			}
			rep := h(r, u)
			b, err := json.Marshal(rep.body)
			if err != nil {
				slog.Error("marshal response", "err", err)
				return http.StatusInternalServerError, []byte(`{"error":"internal error"}`)
			}
			return rep.status, b
		}
		if !idempotent {
			status, body := run()
			write(w, status, body)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			write(w, http.StatusBadRequest, []byte(`{"error":"Idempotency-Key header is required for destructive operations"}`))
			return
		}
		scoped := u.Name + "|" + r.Method + "|" + r.URL.Path + "|" + key
		prev, err := s.Store.ClaimIdempotency(r.Context(), scoped)
		if err != nil {
			slog.Error("claim idempotency key", "err", err)
			write(w, http.StatusServiceUnavailable, []byte(`{"error":"store unavailable"}`))
			return
		}
		if prev != nil {
			if !prev.Done {
				write(w, http.StatusConflict, []byte(`{"error":"a request with this Idempotency-Key is still in progress"}`))
				return
			}
			w.Header().Set("Idempotent-Replayed", "true")
			write(w, prev.Status, prev.Body)
			return
		}
		status, body := run()
		// Detach from the request context: the outcome must be recorded even if the client hung up.
		if err := s.Store.CompleteIdempotency(context.WithoutCancel(r.Context()), scoped, store.Result{Status: status, Body: body}); err != nil {
			slog.Error("complete idempotency key", "err", err)
		}
		write(w, status, body)
	})
}

// clusterView adds per-operation permission so the UI never re-derives capability rules.
type clusterView struct {
	domain.Cluster
	Operations map[domain.Operation]permission `json:"operations"`
}

type permission struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

func view(c domain.Cluster) clusterView {
	v := clusterView{Cluster: c, Operations: map[domain.Operation]permission{}}
	for _, op := range domain.Operations {
		ok, why := c.Allowed(op)
		v.Operations[op] = permission{ok, why}
	}
	return v
}

func engineErr(err error) reply {
	switch {
	case errors.Is(err, engine.ErrNotFound):
		return fail(http.StatusNotFound, err.Error())
	case errors.Is(err, engine.ErrConflict), errors.Is(err, engine.ErrNotReady):
		return fail(http.StatusConflict, err.Error())
	}
	slog.Error("engine", "err", err)
	return fail(http.StatusBadGateway, "lifecycle engine error")
}

func (s *Server) me(_ *http.Request, u auth.User) reply {
	return reply{http.StatusOK, map[string]any{"name": u.Name, "admin": u.InGroup(s.AdminGroup)}}
}

func (s *Server) versions(*http.Request, auth.User) reply {
	return reply{http.StatusOK, upgrade.Versions()}
}

func (s *Server) listClusters(r *http.Request, _ auth.User) reply {
	cs, err := s.Engine.ListClusters(r.Context())
	if err != nil {
		return engineErr(err)
	}
	out := make([]clusterView, len(cs))
	for i, c := range cs {
		out[i] = view(c)
	}
	return reply{http.StatusOK, out}
}

func (s *Server) getCluster(r *http.Request, _ auth.User) reply {
	c, err := s.Engine.GetCluster(r.Context(), r.PathValue("id"))
	if err != nil {
		return engineErr(err)
	}
	return reply{http.StatusOK, view(c)}
}

func (s *Server) listAudit(r *http.Request, _ auth.User) reply {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 500 {
		limit = 100
	}
	recs, err := s.Store.ListAudit(r.Context(), limit)
	if err != nil {
		slog.Error("list audit", "err", err)
		return fail(http.StatusServiceUnavailable, "store unavailable")
	}
	return reply{http.StatusOK, recs}
}

func decode(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// lifecycle runs op under the service identity, bracketed by audit records. If the
// "attempt" record cannot be written the operation is refused.
func (s *Server) lifecycle(ctx context.Context, u auth.User, action, clusterID string, before, after any, op func() error) error {
	rec := audit.Record{Actor: u.Name, Action: action, ClusterID: clusterID, Before: mustJSON(before), After: mustJSON(after)}
	rec.Result = audit.ResultAttempt
	if err := s.Store.AddAudit(ctx, rec); err != nil {
		slog.Error("audit attempt", "err", err)
		return errAuditUnavailable
	}
	err := op()
	rec.Result = audit.ResultSuccess
	if err != nil {
		rec.Result, rec.Error = audit.ResultFailure, err.Error()
	}
	if aerr := s.Store.AddAudit(context.WithoutCancel(ctx), rec); aerr != nil {
		slog.Error("audit result", "err", aerr, "action", action, "cluster", clusterID, "result", rec.Result)
	}
	return err
}

var errAuditUnavailable = errors.New("audit trail unavailable")

func mustJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func (s *Server) lifecycleErr(err error) reply {
	if errors.Is(err, errAuditUnavailable) {
		return fail(http.StatusServiceUnavailable, "audit trail unavailable; operation refused")
	}
	return engineErr(err)
}

func (s *Server) createCluster(r *http.Request, u auth.User) reply {
	var req domain.CreateRequest
	if err := decode(r, &req); err != nil {
		return fail(http.StatusBadRequest, "invalid JSON body")
	}
	if err := req.Validate(upgrade.Versions()); err != nil {
		return fail(http.StatusUnprocessableEntity, err.Error())
	}
	var created domain.Cluster
	err := s.lifecycle(r.Context(), u, "create", req.Name, nil, req, func() (err error) {
		created, err = s.Engine.CreateCluster(r.Context(), req)
		return err
	})
	if err != nil {
		return s.lifecycleErr(err)
	}
	return reply{http.StatusCreated, view(created)}
}

func (s *Server) upgradeCluster(r *http.Request, u auth.User) reply {
	var req struct {
		KubernetesVersion string `json:"kubernetesVersion"`
	}
	if err := decode(r, &req); err != nil {
		return fail(http.StatusBadRequest, "invalid JSON body")
	}
	ctx, id := r.Context(), r.PathValue("id")
	c, err := s.Engine.GetCluster(ctx, id)
	if err != nil {
		return engineErr(err)
	}
	// Capability and upgradeOwner gate first, so we never even probe a cluster we don't own.
	if ok, why := c.Allowed(domain.OpUpgrade); !ok {
		return fail(http.StatusConflict, "upgrade not permitted", why)
	}
	snap, err := s.Engine.LatestSnapshot(ctx, id)
	if err != nil {
		return engineErr(err)
	}
	ready, total, err := s.Engine.NodeHealth(ctx, id)
	if err != nil {
		return engineErr(err)
	}
	if f := upgrade.Check(c, req.KubernetesVersion, upgrade.Preflight{LatestSnapshot: snap, ReadyNodes: ready, TotalNodes: total}, s.Now()); len(f) > 0 {
		return fail(http.StatusPreconditionFailed, "pre-flight checks failed", f...)
	}
	after := c
	after.KubernetesVersion = req.KubernetesVersion
	if err := s.lifecycle(ctx, u, "upgrade", id, c, after, func() error {
		return s.Engine.UpgradeCluster(ctx, id, req.KubernetesVersion)
	}); err != nil {
		return s.lifecycleErr(err)
	}
	return reply{http.StatusAccepted, view(after)}
}

func (s *Server) deleteCluster(r *http.Request, u auth.User) reply {
	ctx, id := r.Context(), r.PathValue("id")
	c, err := s.Engine.GetCluster(ctx, id)
	if err != nil {
		return engineErr(err)
	}
	if ok, why := c.Allowed(domain.OpDelete); !ok {
		return fail(http.StatusConflict, "delete not permitted", why)
	}
	if err := s.lifecycle(ctx, u, "delete", id, c, nil, func() error { return s.Engine.DeleteCluster(ctx, id) }); err != nil {
		return s.lifecycleErr(err)
	}
	return reply{http.StatusAccepted, map[string]string{"status": "deleting"}}
}

func (s *Server) importCluster(r *http.Request, u auth.User) reply {
	var req domain.ImportRequest
	if err := decode(r, &req); err != nil {
		return fail(http.StatusBadRequest, "invalid JSON body")
	}
	if err := req.Validate(); err != nil {
		return fail(http.StatusUnprocessableEntity, err.Error())
	}
	var created domain.Cluster
	err := s.lifecycle(r.Context(), u, "import", req.Name, nil, req, func() (err error) {
		created, err = s.Engine.ImportCluster(r.Context(), req)
		return err
	})
	if err != nil {
		return s.lifecycleErr(err)
	}
	return reply{http.StatusCreated, view(created)}
}

// registration is read-only and holds no secret: the token stays in the referenced Secret.
func (s *Server) registration(r *http.Request, _ auth.User) reply {
	ctx, id := r.Context(), r.PathValue("id")
	c, err := s.Engine.GetCluster(ctx, id)
	if err != nil {
		return engineErr(err)
	}
	if ok, why := c.Allowed(domain.OpRegister); !ok {
		return fail(http.StatusConflict, "registration not permitted", why)
	}
	reg, err := s.Engine.Registration(ctx, id)
	if err != nil {
		return engineErr(err)
	}
	return reply{http.StatusOK, reg}
}
