package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/emresahna/heimdall/internal/config"
	"github.com/emresahna/heimdall/internal/metrics"
	"github.com/emresahna/heimdall/internal/models"
	"github.com/emresahna/heimdall/internal/transport"
)

const (
	maxRetries  = 3
	retryDelay1 = 100 * time.Millisecond
	retryDelay2 = 1 * time.Second
	retryDelay3 = 5 * time.Second
)

var retryDelays = []time.Duration{retryDelay1, retryDelay2, retryDelay3}

type DB struct {
	conn driver.Conn
	cb   *transport.CircuitBreaker
}

func NewClickHouse(cfg config.ClickHouseConfig) (*DB, error) {
	settings := clickhouse.Settings{
		"max_execution_time": int(cfg.MaxExecutionTime.Seconds()),
	}
	if cfg.AsyncInsert {
		settings["async_insert"] = 1
		settings["wait_for_async_insert"] = 0
	}

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{
			cfg.Addr,
		},
		Auth: clickhouse.Auth{
			Database: cfg.DB,
			Username: cfg.User,
			Password: cfg.Password,
		},
		Settings: settings,
	})
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, err
	}

	db := &DB{conn: conn}

	// Initialize circuit breaker for ClickHouse operations
	if cfg.CBThreshold > 0 {
		db.cb = transport.NewCircuitBreakerWithComponent(
			cfg.CBThreshold,
			cfg.CBResetTimeout,
			"clickhouse",
		)
	}

	return db, nil
}

func (db *DB) Migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS http_logs (
		timestamp DateTime64(9),
		pid UInt32,
		tid UInt32,
		fd Int32,
		cgroup_id UInt64,
		type String,
		status UInt32,
		method String,
		path String,
		payload String,
		duration_ns UInt64,
		node String,
		namespace String,
		pod String,
		container String,
		container_id String
	) ENGINE = MergeTree()
	PARTITION BY toDate(timestamp)
	ORDER BY (timestamp, pid, fd)
	TTL timestamp + INTERVAL 7 DAY
	`
	if err := db.conn.Exec(context.Background(), schema); err != nil {
		return err
	}

	columns := []string{
		"tid UInt32",
		"fd Int32",
		"cgroup_id UInt64",
		"node String",
		"namespace String",
		"pod String",
		"container String",
		"container_id String",
	}
	for _, col := range columns {
		stmt := fmt.Sprintf("ALTER TABLE http_logs ADD COLUMN IF NOT EXISTS %s", col)
		if err := db.conn.Exec(context.Background(), stmt); err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) InsertBatch(logs []models.LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	// Use circuit breaker if configured
	if db.cb != nil {
		return db.cb.Execute(context.Background(), func() error {
			return db.insertWithRetry(logs)
		})
	}

	// Fallback to direct insert with retry
	return db.insertWithRetry(logs)
}

func (db *DB) insertWithRetry(logs []models.LogEntry) error {
	start := time.Now()
	defer func() {
		metrics.DBInsertLatency.Observe(time.Since(start).Seconds())
	}()

	var lastErr error

	// First attempt (no backoff)
	lastErr = db.insertBatchWithRetry(logs)
	if lastErr == nil {
		return nil
	}

	log.Printf("ClickHouse insert attempt 1/%d failed: %v", maxRetries, lastErr)

	// Retry with exponential backoff for remaining attempts
	for attempt := 1; attempt < maxRetries; attempt++ {
		log.Printf("ClickHouse insert retry %d/%d after error: %v", attempt, maxRetries-1, lastErr)
		time.Sleep(retryDelays[attempt-1])

		lastErr = db.insertBatchWithRetry(logs)
		if lastErr == nil {
			return nil
		}

		log.Printf("ClickHouse insert attempt %d/%d failed: %v", attempt+1, maxRetries, lastErr)
	}

	// All retries exhausted
	log.Printf("ClickHouse insert failed after %d attempts, dropping %d logs", maxRetries, len(logs))
	metrics.ClickHouseInsertFailures.Inc()
	return lastErr
}

type QueryFilter struct {
	From      time.Time
	To        time.Time
	Limit     int
	Offset    int
	Method    string
	Status    *uint32
	Namespace string
	Pod       string
	Path      string
}

func (db *DB) QueryLogs(ctx context.Context, f QueryFilter) ([]models.LogEntry, error) {
	conditions := []string{"timestamp >= ?", "timestamp <= ?"}
	args := []any{f.From, f.To}

	if f.Method != "" {
		conditions = append(conditions, "method = ?")
		args = append(args, f.Method)
	}
	if f.Status != nil {
		conditions = append(conditions, "status = ?")
		args = append(args, *f.Status)
	}
	if f.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, f.Namespace)
	}
	if f.Pod != "" {
		conditions = append(conditions, "pod = ?")
		args = append(args, f.Pod)
	}
	if f.Path != "" {
		conditions = append(conditions, "path LIKE ?")
		args = append(args, "%"+f.Path+"%")
	}

	query := `
		SELECT
			timestamp, pid, tid, fd, cgroup_id, type, status, method, path,
			payload, duration_ns, node, namespace, pod, container, container_id
		FROM http_logs
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`

	args = append(args, f.Limit, f.Offset)

	rows, err := db.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.LogEntry
	for rows.Next() {
		var entry models.LogEntry
		if err := rows.Scan(
			&entry.Timestamp,
			&entry.Pid,
			&entry.Tid,
			&entry.Fd,
			&entry.CgroupID,
			&entry.Type,
			&entry.Status,
			&entry.Method,
			&entry.Path,
			&entry.Payload,
			&entry.DurationNs,
			&entry.Node,
			&entry.Namespace,
			&entry.Pod,
			&entry.Container,
			&entry.ContainerID,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

func (db *DB) insertBatchWithRetry(logs []models.LogEntry) error {
	ctx := context.Background()

	batch, err := db.conn.PrepareBatch(ctx, `
		INSERT INTO http_logs (
			timestamp, pid, tid, fd, cgroup_id, type, status, method, path,
			payload, duration_ns, node, namespace, pod, container, container_id
		)`)
	if err != nil {
		metrics.DBInsertFailures.Inc()
		return err
	}

	for _, entry := range logs {
		err := batch.Append(
			entry.Timestamp,
			entry.Pid,
			entry.Tid,
			entry.Fd,
			entry.CgroupID,
			entry.Type,
			entry.Status,
			entry.Method,
			entry.Path,
			entry.Payload,
			entry.DurationNs,
			entry.Node,
			entry.Namespace,
			entry.Pod,
			entry.Container,
			entry.ContainerID,
		)
		if err != nil {
			metrics.DBInsertFailures.Inc()
			return err
		}
	}

	if err := batch.Send(); err != nil {
		metrics.DBInsertFailures.Inc()
		return err
	}
	return nil
}
