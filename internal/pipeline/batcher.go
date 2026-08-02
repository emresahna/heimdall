package pipeline

import (
	"context"
	"log"
	"time"

	"github.com/emresahna/heimdall/internal/config"
	"github.com/emresahna/heimdall/internal/metrics"
	"github.com/emresahna/heimdall/internal/models"
	"github.com/emresahna/heimdall/internal/transport"
)

type Batcher struct {
	in            chan models.LogEntry
	batchSize     int
	flushInterval time.Duration
	retryBackoff  time.Duration
	sender        transport.Sender
	diagnostics   *Diagnostics
	node          string
}

func NewBatcher(
	cfg config.BatcherConfig,
	sender transport.Sender,
	diagnostics *Diagnostics,
	node string,
) *Batcher {
	return &Batcher{
		in:            make(chan models.LogEntry, cfg.MaxQueue),
		batchSize:     cfg.BatchSize,
		flushInterval: cfg.FlushInterval,
		retryBackoff:  cfg.RetryBackoff,
		sender:        sender,
		diagnostics:   diagnostics,
		node:          node,
	}
}

func (b *Batcher) Enqueue(entry models.LogEntry) {
	select {
	case b.in <- entry:
	default:
		metrics.QueueDropsTotal.WithLabelValues(b.node).Inc()
		if b.diagnostics != nil {
			b.diagnostics.IncEnqueueDrops()
		}
		log.Printf("batch queue full, dropping log")
	}
}

func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	batch := make([]models.LogEntry, 0, b.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := b.sendWithRetry(ctx, batch); err != nil {
			log.Printf("failed to send batch: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case entry := <-b.in:
			batch = append(batch, entry)
			if len(batch) >= b.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (b *Batcher) sendWithRetry(ctx context.Context, batch []models.LogEntry) error {
	var err error

	for range 3 {
		if err = b.sender.Send(ctx, batch); err == nil {
			metrics.BatchesSentTotal.WithLabelValues(b.node).Inc()
			if b.diagnostics != nil {
				b.diagnostics.IncBatchesSent()
			}
			return nil
		}

		metrics.SendFailuresTotal.WithLabelValues(b.node).Inc()
		if b.diagnostics != nil {
			b.diagnostics.IncSendFailures()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.retryBackoff):
		}
	}

	return err
}
