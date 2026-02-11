package agent

import (
	"context"
	"log"
	"time"

	"github.com/emresahna/heimdall/internal/model"
)

const (
	defaultBatchSize     = 200 * time.Millisecond
	defaultFlushInterval = 2 * time.Second
)

type Batcher struct {
	in            chan model.LogEntry
	batchSize     int
	flushInterval time.Duration
	sender        Sender
}

func NewBatcher(batchSize int, flushInterval time.Duration, maxQueue int, sender Sender) *Batcher {
	if batchSize <= 0 {
		batchSize = 200
	}
	if maxQueue <= 0 {
		maxQueue = 1000
	}
	if flushInterval <= 0 {
		flushInterval = defaultFlushInterval
	}

	return &Batcher{
		in:            make(chan model.LogEntry, maxQueue),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		sender:        sender,
	}
}

func (b *Batcher) Enqueue(entry model.LogEntry) {
	select {
	case b.in <- entry:
	default:
		log.Printf("batch queue full, dropping log")
	}
}

func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	batch := make([]model.LogEntry, 0, b.batchSize)

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

func (b *Batcher) sendWithRetry(ctx context.Context, batch []model.LogEntry) error {
	var err error
	backoff := defaultBatchSize

	for attempt := 0; attempt < 3; attempt++ {
		err = b.sender.Send(ctx, batch)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}

	return err
}
