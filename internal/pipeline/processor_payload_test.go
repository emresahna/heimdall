package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emresahna/heimdall/internal/config"
	"github.com/emresahna/heimdall/internal/correlation"
	"github.com/emresahna/heimdall/internal/models"
	"github.com/emresahna/heimdall/internal/redact"
)

type spyRedactor struct{}

func (spyRedactor) Redact([]byte) []byte { return []byte("[REDACTED]") }

type stubEnricher struct{}

func (stubEnricher) Enrich(_ context.Context, _ uint32, _ uint64, _ *models.LogEntry) {}

func newTestProcessor(payloadCap int, r redact.Redactor) *Processor {
	correlator := correlation.NewCorrelator(config.CorrelatorConfig{TTL: 30 * time.Second})
	batcher := NewBatcher(config.BatcherConfig{MaxQueue: 10}, nil, nil, "node-test")
	return NewProcessor(context.Background(), correlator, stubEnricher{}, batcher, "node-test", payloadCap, r, nil)
}

func requestEvent(pid uint32, fd int32, data string) models.Event {
	return models.Event{
		Timestamp: time.Now(),
		Pid:       pid,
		Tid:       pid,
		Fd:        fd,
		Direction: models.DirectionRequest,
		Data:      []byte(data),
	}
}

func responseEvent(pid uint32, fd int32, data string) models.Event {
	return models.Event{
		Timestamp: time.Now(),
		Pid:       pid,
		Tid:       pid,
		Fd:        fd,
		Direction: models.DirectionResponse,
		Data:      []byte(data),
	}
}

func TestProcessorPayloadCarriedAndRedacted(t *testing.T) {
	p := newTestProcessor(128, spyRedactor{})
	p.HandleEvent(requestEvent(7, 3, "GET /api/orders HTTP/1.1\r\nAuthorization: Bearer secret"))
	p.HandleEvent(responseEvent(7, 3, "HTTP/1.1 200 OK\r\n"))

	entry := <-p.batcher.in
	if entry.Payload != "[REDACTED]" {
		t.Fatalf("expected redacted payload, got %q", entry.Payload)
	}
	if len(entry.Payload) > 100 {
		t.Fatalf("payload exceeded cap: %d", len(entry.Payload))
	}
}

func TestPayloadOffByDefault(t *testing.T) {
	p := newTestProcessor(0, spyRedactor{})
	p.HandleEvent(requestEvent(8, 2, "GET /a HTTP/1.1\r\nHost: x"))
	p.HandleEvent(responseEvent(8, 2, "HTTP/1.1 200 OK\r\n"))

	entry := <-p.batcher.in
	if entry.Payload != "" {
		t.Fatalf("expected empty payload when sampling off, got %q", entry.Payload)
	}
}

func TestPayloadFailClosedWithoutRedactor(t *testing.T) {
	p := newTestProcessor(100, nil)
	p.HandleEvent(requestEvent(9, 4, "GET /secret HTTP/1.1\r\nX-Api-Key: abc"))
	p.HandleEvent(responseEvent(9, 4, "HTTP/1.1 200 OK\r\n"))

	entry := <-p.batcher.in
	if entry.Payload != "" {
		t.Fatalf("expected fail-closed empty payload without redactor, got %q", entry.Payload)
	}
	if !strings.Contains(entry.Path, "secret") {
		t.Fatalf("request path should still be captured: %q", entry.Path)
	}
}

func TestPayloadBoundedToCap(t *testing.T) {
	longBody := strings.Repeat("x", 500)
	p := newTestProcessor(128, spyRedactor{})
	p.HandleEvent(requestEvent(10, 5, "GET /big HTTP/1.1\r\n"+longBody))
	p.HandleEvent(responseEvent(10, 5, "HTTP/1.1 200 OK\r\n"))

	<-p.batcher.in
}
