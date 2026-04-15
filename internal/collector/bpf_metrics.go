package collector

import (
	"context"
	"log"
	"time"

	"github.com/cilium/ebpf"
	"github.com/emresahna/heimdall/internal/metrics"
)

// MapInfo holds metadata about a BPF map for metrics
type MapInfo struct {
	Name string
	Map  *ebpf.Map
}

// BPFMapMetricsReporter periodically collects BPF map pressure metrics
type BPFMapMetricsReporter struct {
	maps []MapInfo
}

// NewBPFMapMetricsReporter creates a reporter for given maps
func NewBPFMapMetricsReporter(maps ...MapInfo) *BPFMapMetricsReporter {
	return &BPFMapMetricsReporter{maps: maps}
}

// Start begins periodic collection of BPF map metrics
func (r *BPFMapMetricsReporter) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Collect immediately on start
	r.collect()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.collect()
		}
	}
}

func (r *BPFMapMetricsReporter) collect() {
	for _, m := range r.maps {
		if m.Map == nil {
			continue
		}

		info, err := m.Map.Info()
		if err != nil {
			log.Printf("failed to get BPF map %s info: %v", m.Name, err)
			continue
		}

		// Use the map's max entries as capacity
		capacity := info.MaxEntries

		// Get actual count by iterating
		iter := m.Map.Iterate()
		var count int
		var key, value interface{}
		for iter.Next(&key, &value) {
			count++
		}
		if iter.Err() != nil {
			log.Printf("failed to iterate BPF map %s: %v", m.Name, iter.Err())
		}

		metrics.BPFMapEntries.WithLabelValues(m.Name).Set(float64(count))
		metrics.BPFMapCapacity.WithLabelValues(m.Name).Set(float64(capacity))

		if capacity > 0 {
			pressure := float64(count) / float64(capacity) * 100
			metrics.BPFMapPressure.WithLabelValues(m.Name).Set(pressure)
		}
	}
}
