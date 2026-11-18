// Copyright 2026 The KubeStack Authors. All rights reserved.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kubestack.dev/kubestack/internal/audit"
	"kubestack.dev/kubestack/internal/auth"
	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine/fake"
	"kubestack.dev/kubestack/internal/store"
)

type userAuth struct{ u auth.User }

func (a userAuth) Authenticate(*http.Request) (auth.User, error) { return a.u, nil }

func setup(groups ...string) (http.Handler, *fake.Engine, *store.Memory) {
	eng, st := fake.Demo(), store.NewMemory()
	s := &Server{Engine: eng, Store: st, AdminGroup: "admins", Now: time.Now,
		Auth: userAuth{auth.User{Name: "alice", Groups: groups}}}
	return s.Routes(""), eng, st
}

func do(h http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestUpgradeFlow(t *testing.T) {
	h, eng, st := setup("admins")
	body := `{"kubernetesVersion":"v1.35.9+rke2r1"}`

	if w := do(h, "POST", "/api/v1/clusters/prod-eu/upgrade", body, ""); w.Code != 400 {
		t.Fatalf("missing idempotency key: got %d", w.Code)
	}
	w := do(h, "POST", "/api/v1/clusters/prod-eu/upgrade", body, "k1")
	if w.Code != http.StatusAccepted {
		t.Fatalf("upgrade: %d %s", w.Code, w.Body)
	}
	if c, _ := eng.GetCluster(context.Background(), "prod-eu"); c.KubernetesVersion != "v1.35.9+rke2r1" {
		t.Fatalf("engine not upgraded: %+v", c)
	}
	// Replay with same key returns the stored result and does not re-run.
	w = do(h, "POST", "/api/v1/clusters/prod-eu/upgrade", body, "k1")
	if w.Code != http.StatusAccepted || w.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %v", w.Code, w.Header())
	}
	recs, _ := st.ListAudit(context.Background(), 10)
	if len(recs) != 2 || recs[0].Result != audit.ResultSuccess || recs[1].Result != audit.ResultAttempt || recs[0].Actor != "alice" {
		t.Fatalf("audit: %+v", recs)
	}
	// Same cluster again with a new key: 1.35 -> 1.35 is a no-op, pre-flight must refuse.
	if w := do(h, "POST", "/api/v1/clusters/prod-eu/upgrade", body, "k2"); w.Code != http.StatusPreconditionFailed {
		t.Fatalf("no-op upgrade: %d %s", w.Code, w.Body)
	}
}

func TestCapabilityRefusals(t *testing.T) {
	h, _, st := setup("admins")
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/v1/clusters/analytics-eks/upgrade", `{"kubernetesVersion":"v1.36.5+rke2r1"}`},
		{"DELETE", "/api/v1/clusters/legacy-k3s", ""},
	} {
		w := do(h, tc.method, tc.path, tc.body, "k")
		var e apiError
		json.Unmarshal(w.Body.Bytes(), &e)
		if w.Code != http.StatusConflict || len(e.Details) == 0 {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	if recs, _ := st.ListAudit(context.Background(), 10); len(recs) != 0 {
		t.Errorf("refused ops must not reach the engine: %+v", recs)
	}
}

func TestPreflightBlocksStaleSnapshot(t *testing.T) {
	h, eng, _ := setup("admins")
	*eng = *fake.New(fake.Cluster{Cluster: mustGet(eng, "prod-eu"), Snapshot: time.Now().Add(-72 * time.Hour), ReadyNodes: 3, TotalNodes: 3})
	w := do(h, "POST", "/api/v1/clusters/prod-eu/upgrade", `{"kubernetesVersion":"v1.35.9+rke2r1"}`, "k")
	if w.Code != http.StatusPreconditionFailed || !strings.Contains(w.Body.String(), "snapshot") {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func mustGet(e *fake.Engine, id string) domain.Cluster {
	c, _ := e.GetCluster(context.Background(), id)
	return c
}

func TestCreateAndRBAC(t *testing.T) {
	h, _, _ := setup() // not in admins
	if w := do(h, "POST", "/api/v1/clusters", `{"name":"x","kubernetesVersion":"v1.35.9+rke2r1"}`, ""); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin create: %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/clusters", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"operations"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}

	h, _, _ = setup("admins")
	if w := do(h, "POST", "/api/v1/clusters", `{"name":"Bad_Name","kubernetesVersion":"v1.35.9+rke2r1"}`, ""); w.Code != 422 {
		t.Fatalf("invalid name: %d", w.Code)
	}
	if w := do(h, "POST", "/api/v1/clusters", `{"name":"edge-1","kubernetesVersion":"v9.9.9"}`, ""); w.Code != 422 {
		t.Fatalf("uncatalogued version: %d", w.Code)
	}
	if w := do(h, "POST", "/api/v1/clusters", `{"name":"edge-1","kubernetesVersion":"v1.35.9+rke2r1"}`, ""); w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/v1/clusters", `{"name":"edge-1","kubernetesVersion":"v1.35.9+rke2r1"}`, ""); w.Code != 409 {
		t.Fatalf("duplicate: %d", w.Code)
	}
}

func TestMe(t *testing.T) {
	h, _, _ := setup("admins")
	w := do(h, "GET", "/api/v1/me", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"admin":true`) || !strings.Contains(w.Body.String(), `"name":"alice"`) {
		t.Fatalf("me: %d %s", w.Code, w.Body)
	}
}
