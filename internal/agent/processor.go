package agent

import (
	"context"
	"time"

	"github.com/emresahna/heimdall/internal/collector"
	"github.com/emresahna/heimdall/internal/model"
)

const maintenanceInterval = 10 * time.Second

type Processor struct {
	ctx        context.Context
	correlator *Correlator
	enricher   Enricher
	batcher    *Batcher
	node       string
	sampleMax  int
}

func NewProcessor(
	ctx context.Context,
	correlator *Correlator,
	enricher Enricher,
	batcher *Batcher,
	node string,
	sampleMax int,
) *Processor {
	return &Processor{
		ctx:        ctx,
		correlator: correlator,
		enricher:   enricher,
		batcher:    batcher,
		node:       node,
		sampleMax:  sampleMax,
	}
}

func (p *Processor) HandleEvent(ev collector.Event) {
	if p.sampleMax > 0 && len(ev.Data) > p.sampleMax {
		ev.Data = ev.Data[:p.sampleMax]
	}
	switch ev.Direction {
	case collector.DirectionRequest:
		method, path, ok := ParseRequestLine(ev.Data)
		if !ok {
			return
		}
		p.correlator.Add(requestEntry{
			Key: requestKey{
				Pid: ev.Pid,
				Fd:  ev.Fd,
			},
			Tid:      ev.Tid,
			CgroupID: ev.CgroupID,
			Method:   method,
			Path:     path,
			Started:  ev.Timestamp,
		})
	case collector.DirectionResponse:
		status, ok := ParseResponseLine(ev.Data)
		if !ok {
			return
		}
		req, ok := p.correlator.Match(ev.Pid, ev.Fd)
		if !ok {
			return
		}

		duration := ev.Timestamp.Sub(req.Started)
		if duration < 0 {
			duration = 0
		}

		entry := model.LogEntry{
			Timestamp:  req.Started,
			Pid:        req.Key.Pid,
			Tid:        req.Tid,
			Fd:         req.Key.Fd,
			CgroupID:   req.CgroupID,
			Type:       "http",
			Status:     status,
			Method:     req.Method,
			Path:       req.Path,
			DurationNs: uint64(duration.Nanoseconds()),
			Node:       p.node,
		}

		p.enricher.Enrich(p.ctx, entry.Pid, entry.CgroupID, &entry)
		p.batcher.Enqueue(entry)
	}
}

func (p *Processor) RunMaintenance(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = maintenanceInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.correlator.Expire(time.Now())
		}
	}
}
