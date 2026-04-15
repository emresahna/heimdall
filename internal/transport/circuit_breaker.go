package transport

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/emresahna/heimdall/internal/metrics"
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
	component    string // for metrics
}

func NewCircuitBreaker(threshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:        Closed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		component:    "sender",
	}
}

func NewCircuitBreakerWithComponent(threshold int, resetTimeout time.Duration, component string) *CircuitBreaker {
	cb := &CircuitBreaker{
		state:        Closed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		component:    component,
	}
	// Initialize metrics
	metrics.CircuitBreakerState.WithLabelValues(component).Set(0)
	return cb
}

func (cb *CircuitBreaker) Execute(ctx context.Context, f func() error) error {
	cb.mu.Lock()
	if cb.state == Open {
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = HalfOpen
			metrics.CircuitBreakerState.WithLabelValues(cb.component).Set(2)
		} else {
			cb.mu.Unlock()
			metrics.CircuitBreakerRejects.WithLabelValues(cb.component).Inc()
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
			metrics.CircuitBreakerState.WithLabelValues(cb.component).Set(1)
			metrics.CircuitBreakerFailures.WithLabelValues(cb.component).Inc()
		}
		return err
	}

	if cb.state == HalfOpen {
		cb.state = Closed
		cb.failureCount = 0
		metrics.CircuitBreakerState.WithLabelValues(cb.component).Set(0)
	}

	return nil
}
