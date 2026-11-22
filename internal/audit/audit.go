// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package audit defines the audit trail record. Records must never contain secrets.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

const (
	ResultAttempt = "attempt" // written before the engine call; if this fails the operation is refused
	ResultSuccess = "success"
	ResultFailure = "failure"
)

type Record struct {
	ID        int64           `json:"id"`
	Time      time.Time       `json:"time"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	ClusterID string          `json:"clusterId"`
	Before    json.RawMessage `json:"before,omitempty"`
	After     json.RawMessage `json:"after,omitempty"`
	Result    string          `json:"result"`
	Error     string          `json:"error,omitempty"`
}

// Sink stores audit records (internal/store implements it).
type Sink interface {
	AddAudit(ctx context.Context, r Record) error
}

var ErrUnavailable = errors.New("audit trail unavailable")

// Run performs a lifecycle operation under the service identity, bracketed by audit records. If the
// "attempt" record cannot be written the operation is refused with ErrUnavailable.
func Run(ctx context.Context, s Sink, actor, action, clusterID string, before, after any, op func() error) error {
	rec := Record{Actor: actor, Action: action, ClusterID: clusterID, Before: rawJSON(before), After: rawJSON(after)}
	rec.Result = ResultAttempt
	if err := s.AddAudit(ctx, rec); err != nil {
		slog.Error("audit attempt", "err", err)
		return ErrUnavailable
	}
	err := op()
	rec.Result = ResultSuccess
	if err != nil {
		rec.Result, rec.Error = ResultFailure, err.Error()
	}
	if aerr := s.AddAudit(context.WithoutCancel(ctx), rec); aerr != nil {
		slog.Error("audit result", "err", aerr, "action", action, "cluster", clusterID, "result", rec.Result)
	}
	return err
}

func rawJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
