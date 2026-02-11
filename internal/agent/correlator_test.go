package agent

import (
	"testing"
	"time"
)

func TestCorrelatorMatch(t *testing.T) {
	corr := NewCorrelator(5 * time.Second)
	req := requestEntry{
		Key:     requestKey{Pid: 1, Fd: 3},
		Method:  "GET",
		Path:    "/healthz",
		Started: time.Now(),
	}
	corr.Add(req)

	got, ok := corr.Match(1, 3)
	if !ok {
		t.Fatalf("expected match")
	}
	if got.Method != "GET" || got.Path != "/healthz" {
		t.Fatalf("unexpected request data")
	}
}

func TestCorrelatorExpire(t *testing.T) {
	corr := NewCorrelator(1 * time.Second)
	req := requestEntry{
		Key:     requestKey{Pid: 2, Fd: 5},
		Started: time.Now().Add(-2 * time.Second),
	}
	corr.Add(req)

	removed := corr.Expire(time.Now())
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
}
