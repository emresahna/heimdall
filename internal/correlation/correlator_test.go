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
		Seqno:   1,
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

// TestCorrelatorFdReuse proves that sequential requests on the same (pid, fd)
// are matched oldest-first, so a reuse/late-response can no longer pair with
// the wrong request.
func TestCorrelatorFdReuse(t *testing.T) {
	corr := NewCorrelator(config.CorrelatorConfig{TTL: 5 * time.Second})
	now := time.Now()
	key := RequestKey{Pid: 42, Fd: 7}

	for i, seq := range []uint32{1, 2, 3} {
		corr.Add(Request{
			Key:     key,
			Seqno:   seq,
			Method:  "GET",
			Path:    "/req" + string(rune('A'+i)),
			Started: now,
		})
	}

	// Same fd reused; responses must pair oldest-first.
	for i, want := range []string{"/reqA", "/reqB", "/reqC"} {
		got, ok := corr.Match(key.Pid, key.Fd)
		if !ok {
			t.Fatalf("expected match %d", i)
		}
		if got.Path != want {
			t.Fatalf("match %d: expected %s, got %s", i, want, got.Path)
		}
	}

	if _, ok := corr.Match(key.Pid, key.Fd); ok {
		t.Fatalf("expected no match after exhausting the FIFO")
	}
}

// TestCorrelatorMatchOldestSeqno verifies the FIFO is keyed on kernel seqno,
// not insertion order, so cross-thread ringbuf delivery cannot mispair.
func TestCorrelatorMatchOldestSeqno(t *testing.T) {
	corr := NewCorrelator(config.CorrelatorConfig{TTL: 5 * time.Second})
	now := time.Now()
	key := RequestKey{Pid: 9, Fd: 1}

	// Delivered out of kernel order (C before A before B).
	corr.Add(Request{Key: key, Seqno: 3, Path: "/c", Started: now})
	corr.Add(Request{Key: key, Seqno: 1, Path: "/a", Started: now})
	corr.Add(Request{Key: key, Seqno: 2, Path: "/b", Started: now})

	for _, want := range []string{"/a", "/b", "/c"} {
		got, ok := corr.Match(key.Pid, key.Fd)
		if !ok || got.Path != want {
			t.Fatalf("expected match for %s, got %q ok=%v", want, got.Path, ok)
		}
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
