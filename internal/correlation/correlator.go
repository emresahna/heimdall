package correlation

import (
	"sync"
	"time"

	"github.com/emresahna/heimdall/internal/config"
)

const (
	// Default number of shards for the correlation map
	DefaultShards = 16
)

type RequestKey struct {
	Pid uint32
	Fd  int32
}

type Request struct {
	Key      RequestKey
	Tid      uint32
	Seqno    uint32
	CgroupID uint64
	Method   string
	Path     string
	Payload  string
	Started  time.Time
}

// shard holds a portion of the correlation map with its own mutex
type shard struct {
	mu       sync.Mutex
	requests map[RequestKey][]Request
}

// Correlator uses sharded maps to reduce contention under high load
type Correlator struct {
	shards []shard
	ttl    time.Duration
	// number of shards - must be power of 2 for efficient modulo
	shardMask uint32
}

// NewCorrelator creates a new Correlator with the specified number of shards
func NewCorrelator(cfg config.CorrelatorConfig) *Correlator {
	shardCount := DefaultShards
	// Ensure shard count is power of 2
	if shardCount > 0 {
		// Round up to next power of 2
		shardCount = 1 << (32 - leadingZeros(uint32(shardCount-1)))
	}

	shards := make([]shard, shardCount)
	for i := range shards {
		shards[i].requests = make(map[RequestKey][]Request)
	}

	return &Correlator{
		shards:    shards,
		ttl:       cfg.TTL,
		shardMask: uint32(shardCount - 1),
	}
}

// shardIndex returns the shard index for a given PID
func (c *Correlator) shardIndex(pid uint32) uint32 {
	return pid & c.shardMask
}

// Add adds a request to the correlation map, keeping the per-key FIFO ordered
// by kernel seqno so the oldest request is always at the head.
func (c *Correlator) Add(req Request) {
	idx := c.shardIndex(req.Key.Pid)
	shard := &c.shards[idx]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	queue := shard.requests[req.Key]
	if len(queue) == 0 {
		shard.requests[req.Key] = []Request{req}
		return
	}

	insertAt := len(queue)
	for i, queued := range queue {
		if req.Seqno < queued.Seqno {
			insertAt = i
			break
		}
	}
	if insertAt == len(queue) {
		shard.requests[req.Key] = append(queue, req)
		return
	}

	queue = append(queue, Request{})
	copy(queue[insertAt+1:], queue[insertAt:])
	queue[insertAt] = req
	shard.requests[req.Key] = queue
}

// Match finds and removes the oldest matching request for a given (pid, fd).
// Responses on one HTTP/1.x connection arrive in request order, so the FIFO
// head is the correct pairing even when the fd has been reused.
func (c *Correlator) Match(pid uint32, fd int32) (Request, bool) {
	idx := c.shardIndex(pid)
	shard := &c.shards[idx]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	key := RequestKey{Pid: pid, Fd: fd}
	queue := shard.requests[key]
	if len(queue) == 0 {
		return Request{}, false
	}

	req := queue[0]
	if len(queue) == 1 {
		delete(shard.requests, key)
	} else {
		shard.requests[key] = queue[1:]
	}
	return req, true
}

// Expire removes expired entries from all shards
func (c *Correlator) Expire(now time.Time) int {
	removed := 0
	for i := range c.shards {
		shard := &c.shards[i]
		shard.mu.Lock()
		for key, queue := range shard.requests {
			kept := queue[:0]
			for _, req := range queue {
				if now.Sub(req.Started) > c.ttl {
					removed++
					continue
				}
				kept = append(kept, req)
			}
			if len(kept) == 0 {
				delete(shard.requests, key)
			} else {
				shard.requests[key] = kept
			}
		}
		shard.mu.Unlock()
	}
	return removed
}

// leadingZeros returns the number of leading zeros in a 32-bit integer
func leadingZeros(n uint32) uint32 {
	if n == 0 {
		return 32
	}
	var count uint32
	if n&0xFFFF0000 == 0 {
		count += 16
		n <<= 16
	}
	if n&0xFF000000 == 0 {
		count += 8
		n <<= 8
	}
	if n&0xF0000000 == 0 {
		count += 4
		n <<= 4
	}
	if n&0xC0000000 == 0 {
		count += 2
		n <<= 2
	}
	if n&0x80000000 == 0 {
		count++
	}
	return count
}
