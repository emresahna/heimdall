package pipeline

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/emresahna/heimdall/internal/correlation"
	"github.com/emresahna/heimdall/internal/enrichment"
	"github.com/emresahna/heimdall/internal/metrics"
	"github.com/emresahna/heimdall/internal/models"
	"github.com/emresahna/heimdall/internal/redact"
)

type Processor struct {
	ctx         context.Context
	correlator  *correlation.Correlator
	enricher    enrichment.Enricher
	batcher     *Batcher
	node        string
	payloadCap  int
	redactor    redact.Redactor
	diagnostics *Diagnostics

	redactorWarn sync.Once
}

func NewProcessor(
	ctx context.Context,
	correlator *correlation.Correlator,
	enricher enrichment.Enricher,
	batcher *Batcher,
	node string,
	payloadCap int,
	redactor redact.Redactor,
	diagnostics *Diagnostics,
) *Processor {
	return &Processor{
		ctx:         ctx,
		correlator:  correlator,
		enricher:    enricher,
		batcher:     batcher,
		node:        node,
		payloadCap:  payloadCap,
		redactor:    redactor,
		diagnostics: diagnostics,
	}
}

func (p *Processor) HandleEvent(ev models.Event) {
	metrics.EventsReadTotal.WithLabelValues(p.node).Inc()
	if p.diagnostics != nil {
		p.diagnostics.IncEventsRead()
	}
	if p.payloadCap > 0 && len(ev.Data) > p.payloadCap {
		ev.Data = ev.Data[:p.payloadCap]
	}
	switch ev.Direction {
	case models.DirectionRequest:
		method, path, ok := parseRequestLine(ev.Data)
		if !ok {
			return
		}
		if p.diagnostics != nil {
			p.diagnostics.IncParsedRequests()
		}
		payload := ""
		if p.payloadCap > 0 {
			if p.redactor == nil {
				p.redactorWarn.Do(func() {
					log.Printf("payload sampling enabled (HTTP_SAMPLE_BYTES=%d) but no redactor configured; no bytes will be stored", p.payloadCap)
				})
			} else {
				payload = string(p.redactor.Redact(ev.Data))
			}
		}
		p.correlator.Add(correlation.Request{
			Key: correlation.RequestKey{
				Pid: ev.Pid,
				Fd:  ev.Fd,
			},
			Tid:      ev.Tid,
			CgroupID: ev.CgroupID,
			Method:   method,
			Path:     path,
			Payload:  payload,
			Started:  ev.Timestamp,
		})
	case models.DirectionResponse:
		status, ok := parseResponseLine(ev.Data)
		if !ok {
			return
		}
		if p.diagnostics != nil {
			p.diagnostics.IncParsedResponses()
		}
		req, ok := p.correlator.Match(ev.Pid, ev.Fd)
		if !ok {
			if p.diagnostics != nil {
				p.diagnostics.IncUnmatchedResponses()
			}
			return
		}
		if p.diagnostics != nil {
			p.diagnostics.IncMatchedResponses()
		}

		duration := ev.Timestamp.Sub(req.Started)
		if duration < 0 {
			duration = 0
		}

		entry := models.LogEntry{
			Timestamp:  req.Started,
			Pid:        req.Key.Pid,
			Tid:        req.Tid,
			Fd:         req.Key.Fd,
			CgroupID:   req.CgroupID,
			Type:       "http",
			Status:     status,
			Method:     req.Method,
			Path:       req.Path,
			Payload:    req.Payload,
			DurationNs: uint64(duration.Nanoseconds()),
			Node:       p.node,
		}

		p.enricher.Enrich(p.ctx, entry.Pid, entry.CgroupID, &entry)
		p.batcher.Enqueue(entry)
	}
}

func (p *Processor) RunMaintenance(ctx context.Context, interval time.Duration) {
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
