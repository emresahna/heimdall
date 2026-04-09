package transport

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)
	ctx := context.Background()

	// Initial state Closed
	err := cb.Execute(ctx, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}

	// First failure
	_ = cb.Execute(ctx, func() error { return errors.New("fail") })
	if cb.state != Closed {
		t.Fatal("should still be closed")
	}

	// Second failure -> Open
	_ = cb.Execute(ctx, func() error { return errors.New("fail") })
	if cb.state != Open {
		t.Fatal("should be open")
	}

	// Immediate call should return ErrCircuitOpen
	err = cb.Execute(ctx, func() error { return nil })
	if err != ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Half-Open -> Success -> Closed
	err = cb.Execute(ctx, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if cb.state != Closed {
		t.Fatal("should be closed again")
	}
}
