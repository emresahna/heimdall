package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Agent Metrics
	EventsReadTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "heimdall_agent_events_read_total",
		Help: "The total number of events read by the collector",
	}, []string{"node"})

	BatchesSentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "heimdall_agent_batches_sent_total",
		Help: "The total number of batches sent to the server",
	}, []string{"node"})

	SendFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "heimdall_agent_send_failures_total",
		Help: "The total number of failures while sending batches",
	}, []string{"node"})

	QueueDropsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "heimdall_agent_queue_drops_total",
		Help: "The total number of logs dropped due to full queue",
	}, []string{"node"})

	// Server Metrics
	EventsReceivedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "heimdall_server_events_received_total",
		Help: "The total number of events received by the server",
	})

	DBInsertLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "heimdall_server_db_insert_latency_seconds",
		Help:    "Latency of database insertions",
		Buckets: prometheus.DefBuckets,
	})

	DBInsertFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "heimdall_server_db_insert_failures_total",
		Help: "The total number of database insertion failures",
	})

	ClickHouseInsertFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "clickhouse_insert_failures_total",
		Help: "The total number of ClickHouse insert failures after retry exhaustion",
	})
)
