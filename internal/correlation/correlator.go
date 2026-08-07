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
	CgroupID uint64
	Method   string
	Path     string
	Payload  string
	Started  time.Time
}

// shard holds a portion of the correlation map with its own mutex
type shard struct {
	mu       sync.Mutex
	requests map[RequestKey]Request
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
		shards[i].requests = make(map[RequestKey]Request)
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

// Add adds a request to the correlation map
func (c *Correlator) Add(req Request) {
	idx := c.shardIndex(req.Key.Pid)
	shard := &c.shards[idx]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	shard.requests[req.Key] = req
}

// Match finds and removes a matching request from the correlation map
func (c *Correlator) Match(pid uint32, fd int32) (Request, bool) {
	idx := c.shardIndex(pid)
	shard := &c.shards[idx]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	key := RequestKey{Pid: pid, Fd: fd}
	req, ok := shard.requests[key]
	if ok {
		delete(shard.requests, key)
	}
	return req, ok
}

// Expire removes expired entries from all shards
func (c *Correlator) Expire(now time.Time) int {
	removed := 0
	for i := range c.shards {
		shard := &c.shards[i]
		shard.mu.Lock()
		for key, req := range shard.requests {
			if now.Sub(req.Started) > c.ttl {
				delete(shard.requests, key)
				removed++
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
