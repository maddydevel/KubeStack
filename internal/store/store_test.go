// Copyright 2026 The KubeStack Authors. All rights reserved.

package store

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"kubestack.dev/kubestack/internal/audit"
)

// Contract shared by both implementations. Postgres runs only when KUBESTACK_TEST_DATABASE_URL is set
// (make test-integration starts one in Docker).
func stores(t *testing.T) map[string]Store {
	s := map[string]Store{"memory": NewMemory()}
	if url := os.Getenv("KUBESTACK_TEST_DATABASE_URL"); url != "" {
		pg, err := NewPostgres(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pg.db.Exec(context.Background(), `TRUNCATE audit_records, idempotency_keys RESTART IDENTITY`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pg.db.Close)
		s["postgres"] = pg
	}
	return s
}

func TestStoreContract(t *testing.T) {
	ctx := context.Background()
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			for i, res := range []string{audit.ResultAttempt, audit.ResultSuccess} {
				r := audit.Record{Actor: "alice", Action: "upgrade", ClusterID: "c1", Result: res, Before: json.RawMessage(`{"v":"1.32"}`)}
				if i == 1 {
					r.After = json.RawMessage(`{"v":"1.33"}`)
				}
				if err := s.AddAudit(ctx, r); err != nil {
					t.Fatal(err)
				}
			}
			recs, err := s.ListAudit(ctx, 10)
			if err != nil || len(recs) != 2 {
				t.Fatalf("list: %v %+v", err, recs)
			}
			if recs[0].Result != audit.ResultSuccess || recs[0].After == nil || recs[1].After != nil || recs[0].Time.IsZero() {
				t.Fatalf("newest-first / json roundtrip broken: %+v", recs)
			}
			if one, _ := s.ListAudit(ctx, 1); len(one) != 1 {
				t.Fatalf("limit ignored: %d", len(one))
			}

			// Exactly one of N concurrent claims wins.
			var wg sync.WaitGroup
			var mu sync.Mutex
			wins := 0
			for range 10 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					prev, err := s.ClaimIdempotency(ctx, "k")
					if err != nil {
						t.Error(err)
					}
					if prev == nil {
						mu.Lock()
						wins++
						mu.Unlock()
					} else if prev.Done {
						t.Error("unfinished key reported done")
					}
				}()
			}
			wg.Wait()
			if wins != 1 {
				t.Fatalf("claims won = %d, want 1", wins)
			}
			if err := s.CompleteIdempotency(ctx, "k", Result{Status: 202, Body: []byte(`{"ok":true}`)}); err != nil {
				t.Fatal(err)
			}
			prev, err := s.ClaimIdempotency(ctx, "k")
			if err != nil || prev == nil || !prev.Done || prev.Status != 202 || string(prev.Body) != `{"ok":true}` {
				t.Fatalf("replay: %+v %v", prev, err)
			}
		})
	}
}
