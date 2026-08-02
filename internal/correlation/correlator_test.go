package correlation

import (
	"net/http"
	"testing"
	"time"

	"github.com/emresahna/heimdall/internal/config"
)

func TestCorrelatorMatch(t *testing.T) {
	corr := NewCorrelator(config.CorrelatorConfig{TTL: 5 * time.Second})
	req := Request{
		Key:     RequestKey{Pid: 1, Fd: 3},
		Method:  "GET",
		Path:    "/healthz",
		Started: time.Now(),
	}
	corr.Add(req)

	got, ok := corr.Match(1, 3)
	if !ok {
		t.Fatalf("expected match")
	}
	if got.Method != http.MethodGet || got.Path != "/healthz" {
		t.Fatalf("unexpected request data")
	}

	// Verify it was deleted after match
	_, ok = corr.Match(1, 3)
	if ok {
		t.Fatalf("expected no match after first match")
	}
}

func TestCorrelatorExpire(t *testing.T) {
	corr := NewCorrelator(config.CorrelatorConfig{TTL: 1 * time.Second})

	// Should expire
	req1 := Request{
		Key:     RequestKey{Pid: 2, Fd: 5},
		Started: time.Now().Add(-2 * time.Second),
	}
	corr.Add(req1)

	// Should NOT expire
	req2 := Request{
		Key:     RequestKey{Pid: 3, Fd: 7},
		Started: time.Now(),
	}
	corr.Add(req2)

	removed := corr.Expire(time.Now())
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}

	// Verify req1 is gone
	_, ok := corr.Match(2, 5)
	if ok {
		t.Fatalf("expected req1 to be expired")
	}

	// Verify req2 is still there
	_, ok = corr.Match(3, 7)
	if !ok {
		t.Fatalf("expected req2 to be still present")
	}
}
