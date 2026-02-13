package pipeline

import (
	"context"
	"log"
	"sync/atomic"
	"time"
)

type Snapshot struct {
	EventsRead         uint64
	ParsedRequests     uint64
	ParsedResponses    uint64
	MatchedResponses   uint64
	UnmatchedResponses uint64
	EnqueueDrops       uint64
	BatchesSent        uint64
	SendFailures       uint64
}

type Diagnostics struct {
	eventsRead         atomic.Uint64
	parsedRequests     atomic.Uint64
	parsedResponses    atomic.Uint64
	matchedResponses   atomic.Uint64
	unmatchedResponses atomic.Uint64
	enqueueDrops       atomic.Uint64
	batchesSent        atomic.Uint64
	sendFailures       atomic.Uint64
}

func NewDiagnostics() *Diagnostics {
	return &Diagnostics{}
}

func (d *Diagnostics) IncEventsRead() {
	d.eventsRead.Add(1)
}

func (d *Diagnostics) IncParsedRequests() {
	d.parsedRequests.Add(1)
}

func (d *Diagnostics) IncParsedResponses() {
	d.parsedResponses.Add(1)
}

func (d *Diagnostics) IncMatchedResponses() {
	d.matchedResponses.Add(1)
}

func (d *Diagnostics) IncUnmatchedResponses() {
	d.unmatchedResponses.Add(1)
}

func (d *Diagnostics) IncEnqueueDrops() {
	d.enqueueDrops.Add(1)
}

func (d *Diagnostics) IncBatchesSent() {
	d.batchesSent.Add(1)
}

func (d *Diagnostics) IncSendFailures() {
	d.sendFailures.Add(1)
}

func (d *Diagnostics) Snapshot() Snapshot {
	return Snapshot{
		EventsRead:         d.eventsRead.Load(),
		ParsedRequests:     d.parsedRequests.Load(),
		ParsedResponses:    d.parsedResponses.Load(),
		MatchedResponses:   d.matchedResponses.Load(),
		UnmatchedResponses: d.unmatchedResponses.Load(),
		EnqueueDrops:       d.enqueueDrops.Load(),
		BatchesSent:        d.batchesSent.Load(),
		SendFailures:       d.sendFailures.Load(),
	}
}

func StartDiagnosticsReporter(ctx context.Context, diagnostics *Diagnostics, interval time.Duration) {
	if diagnostics == nil || interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	last := diagnostics.Snapshot()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := diagnostics.Snapshot()
			log.Printf(
				"agent diagnostics total(events=%d req=%d resp=%d matched=%d unmatched=%d drops=%d batches=%d send_failures=%d) delta(events=%d req=%d resp=%d matched=%d unmatched=%d drops=%d batches=%d send_failures=%d)",
				current.EventsRead,
				current.ParsedRequests,
				current.ParsedResponses,
				current.MatchedResponses,
				current.UnmatchedResponses,
				current.EnqueueDrops,
				current.BatchesSent,
				current.SendFailures,
				current.EventsRead-last.EventsRead,
				current.ParsedRequests-last.ParsedRequests,
				current.ParsedResponses-last.ParsedResponses,
				current.MatchedResponses-last.MatchedResponses,
				current.UnmatchedResponses-last.UnmatchedResponses,
				current.EnqueueDrops-last.EnqueueDrops,
				current.BatchesSent-last.BatchesSent,
				current.SendFailures-last.SendFailures,
			)
			last = current
		}
	}
}
