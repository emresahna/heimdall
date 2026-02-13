package correlation

import (
	"sync"
	"time"
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
	Started  time.Time
}

type Correlator struct {
	mu       sync.Mutex
	ttl      time.Duration
	requests map[RequestKey]Request
}

func NewCorrelator(ttl time.Duration) *Correlator {
	return &Correlator{
		ttl:      ttl,
		requests: make(map[RequestKey]Request),
	}
}

func (c *Correlator) Add(req Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests[req.Key] = req
}

func (c *Correlator) Match(pid uint32, fd int32) (Request, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := RequestKey{Pid: pid, Fd: fd}
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
