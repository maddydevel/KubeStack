// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package audit defines the audit trail record. Records must never contain secrets.
package audit

import (
	"encoding/json"
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
