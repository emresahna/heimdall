package agent

import (
	"sync"
	"time"
)

type requestKey struct {
	Pid uint32
	Fd  int32
}

type requestEntry struct {
	Key      requestKey
	Tid      uint32
	CgroupID uint64
	Method   string
	Path     string
	Started  time.Time
}

type Correlator struct {
	mu       sync.Mutex
	ttl      time.Duration
	requests map[requestKey]requestEntry
}

func NewCorrelator(ttl time.Duration) *Correlator {
	return &Correlator{
		ttl:      ttl,
		requests: make(map[requestKey]requestEntry),
	}
}

func (c *Correlator) Add(req requestEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests[req.Key] = req
}

func (c *Correlator) Match(pid uint32, fd int32) (requestEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := requestKey{Pid: pid, Fd: fd}
	req, ok := c.requests[key]
	if ok {
		delete(c.requests, key)
	}
	return req, ok
}

func (c *Correlator) Expire(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for key, req := range c.requests {
		if now.Sub(req.Started) > c.ttl {
			delete(c.requests, key)
			removed++
		}
	}
	return removed
}
