package correlation

import (
	"testing"
	"time"

	"github.com/emresahna/heimdall/internal/config"
)

func BenchmarkCorrelatorAddMatch(b *testing.B) {
	corr := NewCorrelator(config.CorrelatorConfig{TTL: 5 * time.Second})
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		pid := uint32(i)
		fd := int32(i % 100)
		req := Request{
			Key:     RequestKey{Pid: pid, Fd: fd},
			Seqno:   1,
			Method:  "GET",
			Path:    "/bench",
			Started: now,
		}
		corr.Add(req)
		if _, ok := corr.Match(pid, fd); !ok {
			b.Fatal("expected match")
		}
	}
}

func BenchmarkCorrelatorExpire(b *testing.B) {
	corr := NewCorrelator(config.CorrelatorConfig{TTL: time.Second})
	now := time.Now()

	// Pre-populate one key with a small FIFO to make Expire iterate non-trivial.
	key := RequestKey{Pid: 1, Fd: 1}
	for i := range 8 {
		corr.Add(Request{Key: key, Seqno: uint32(i), Started: now})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		corr.Expire(now)
	}
}
