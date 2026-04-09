package transport

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

type CircuitBreaker struct {
	mu           sync.Mutex
	state        State
	failureCount int
	threshold    int
	resetTimeout time.Duration
	lastFailure  time.Time
}

func NewCircuitBreaker(threshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:        Closed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

func (cb *CircuitBreaker) Execute(ctx context.Context, f func() error) error {
	cb.mu.Lock()
	if cb.state == Open {
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = HalfOpen
		} else {
			cb.mu.Unlock()
			return ErrCircuitOpen
		}
	}
	cb.mu.Unlock()

	err := f()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		if cb.failureCount >= cb.threshold {
			cb.state = Open
			cb.lastFailure = time.Now()
		}
		return err
	}

	if cb.state == HalfOpen {
		cb.state = Closed
		cb.failureCount = 0
	}

	return nil
}
