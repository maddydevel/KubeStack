// Copyright 2026 The KubeStack Authors. All rights reserved.

package audit

import (
	"context"
	"errors"
	"testing"
)

type sink struct {
	recs []Record
	fail bool
}

func (s *sink) AddAudit(ctx context.Context, r Record) error {
	if s.fail {
		return errors.New("down")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.recs = append(s.recs, r)
	return nil
}

func TestRunRefusesWhenAttemptCannotBeRecorded(t *testing.T) {
	ran := false
	err := Run(context.Background(), &sink{fail: true}, "alice", "delete", "c1", nil, nil, func() error { ran = true; return nil })
	if !errors.Is(err, ErrUnavailable) || ran {
		t.Fatalf("err=%v ran=%v; want ErrUnavailable and op not run", err, ran)
	}
}

func TestRunRecordsAttemptThenOutcome(t *testing.T) {
	for _, tc := range []struct {
		opErr      error
		wantResult string
	}{{nil, ResultSuccess}, {errors.New("boom"), ResultFailure}} {
		s := &sink{}
		err := Run(context.Background(), s, "alice", "upgrade", "c1", map[string]string{"v": "1.30"}, map[string]string{"v": "1.31"}, func() error { return tc.opErr })
		if !errors.Is(err, tc.opErr) {
			t.Fatalf("err=%v, want %v", err, tc.opErr)
		}
		if len(s.recs) != 2 || s.recs[0].Result != ResultAttempt || s.recs[1].Result != tc.wantResult {
			t.Fatalf("records=%+v", s.recs)
		}
		r := s.recs[1]
		if r.Actor != "alice" || r.ClusterID != "c1" || string(r.Before) != `{"v":"1.30"}` || string(r.After) != `{"v":"1.31"}` {
			t.Fatalf("record fields=%+v", r)
		}
		if tc.opErr != nil && r.Error != "boom" {
			t.Fatalf("error not recorded: %+v", r)
		}
	}
}

func TestRunRecordsOutcomeAfterClientCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &sink{}
	_ = Run(ctx, s, "alice", "restore", "c1", nil, nil, func() error { cancel(); return nil })
	if len(s.recs) != 2 || s.recs[1].Result != ResultSuccess {
		t.Fatalf("outcome lost after cancel: %+v", s.recs)
	}
}
