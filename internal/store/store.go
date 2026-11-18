// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package store persists the audit trail and idempotency keys (PostgreSQL, or memory for dev/tests).
package store

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"kubestack.dev/kubestack/internal/audit"
)

// Result is a stored response for an idempotency key. Done=false means still in progress.
type Result struct {
	Done   bool
	Status int
	Body   []byte
}

type Store interface {
	AddAudit(ctx context.Context, r audit.Record) error
	ListAudit(ctx context.Context, limit int) ([]audit.Record, error)
	// ClaimIdempotency atomically claims key. It returns nil if newly claimed, else the prior result.
	ClaimIdempotency(ctx context.Context, key string) (*Result, error)
	CompleteIdempotency(ctx context.Context, key string, r Result) error
}

// ---- memory ----

type Memory struct {
	mu   sync.Mutex
	recs []audit.Record
	idem map[string]Result
}

func NewMemory() *Memory { return &Memory{idem: map[string]Result{}} }

func (m *Memory) AddAudit(_ context.Context, r audit.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ID, r.Time = int64(len(m.recs)+1), time.Now().UTC()
	m.recs = append(m.recs, r)
	return nil
}

func (m *Memory) ListAudit(_ context.Context, limit int) ([]audit.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := slices.Clone(m.recs)
	slices.Reverse(out)
	return out[:min(limit, len(out))], nil
}

func (m *Memory) ClaimIdempotency(_ context.Context, key string) (*Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.idem[key]; ok {
		return &r, nil
	}
	m.idem[key] = Result{}
	return nil, nil
}

func (m *Memory) CompleteIdempotency(_ context.Context, key string, r Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.Done = true
	m.idem[key] = r
	return nil
}

// ---- postgres ----

//go:embed schema.sql
var schema string

type Postgres struct{ db *pgxpool.Pool }

func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Postgres{db: db}, nil
}

func (p *Postgres) AddAudit(ctx context.Context, r audit.Record) error {
	_, err := p.db.Exec(ctx, `INSERT INTO audit_records (actor, action, cluster_id, before, after, result, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, r.Actor, r.Action, r.ClusterID, nullJSON(r.Before), nullJSON(r.After), r.Result, r.Error)
	return err
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (p *Postgres) ListAudit(ctx context.Context, limit int) ([]audit.Record, error) {
	rows, err := p.db.Query(ctx, `SELECT id, time, actor, action, cluster_id, coalesce(before::text,''), coalesce(after::text,''), result, error
		FROM audit_records ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (audit.Record, error) {
		var r audit.Record
		var before, after string
		err := row.Scan(&r.ID, &r.Time, &r.Actor, &r.Action, &r.ClusterID, &before, &after, &r.Result, &r.Error)
		if before != "" {
			r.Before = []byte(before)
		}
		if after != "" {
			r.After = []byte(after)
		}
		return r, err
	})
}

func (p *Postgres) ClaimIdempotency(ctx context.Context, key string) (*Result, error) {
	tag, err := p.db.Exec(ctx, `INSERT INTO idempotency_keys (key) VALUES ($1) ON CONFLICT DO NOTHING`, key)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}
	var r Result
	var status *int
	err = p.db.QueryRow(ctx, `SELECT done, status, body FROM idempotency_keys WHERE key=$1`, key).Scan(&r.Done, &status, &r.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("idempotency key vanished during claim")
	}
	if status != nil {
		r.Status = *status
	}
	return &r, err
}

func (p *Postgres) CompleteIdempotency(ctx context.Context, key string, r Result) error {
	_, err := p.db.Exec(ctx, `UPDATE idempotency_keys SET done=true, status=$2, body=$3 WHERE key=$1`, key, r.Status, r.Body)
	return err
}
