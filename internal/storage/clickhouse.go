package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/emresahna/heimdall/internal/telemetry"
)

type Config struct {
	Addr     string
	Database string
	User     string
	Password string
}

type DB struct {
	conn driver.Conn
}

func NewClickHouse(cfg Config) (*DB, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{
			cfg.Addr,
		},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
	})
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, err
	}

	return &DB{conn: conn}, nil
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

func (db *DB) InsertBatch(logs []telemetry.LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	ctx := context.Background()

	batch, err := db.conn.PrepareBatch(ctx, `
		INSERT INTO http_logs (
			timestamp, pid, tid, fd, cgroup_id, type, status, method, path,
			payload, duration_ns, node, namespace, pod, container, container_id
		)`)
	if err != nil {
		return err
	}

	for _, log := range logs {
		err := batch.Append(
			log.Timestamp,
			log.Pid,
			log.Tid,
			log.Fd,
			log.CgroupID,
			log.Type,
			log.Status,
			log.Method,
			log.Path,
			log.Payload,
			log.DurationNs,
			log.Node,
			log.Namespace,
			log.Pod,
			log.Container,
			log.ContainerID,
		)
		if err != nil {
			return err
		}
	}

	return batch.Send()
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

func (db *DB) QueryLogs(ctx context.Context, f QueryFilter) ([]telemetry.LogEntry, error) {
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

	var entries []telemetry.LogEntry
	for rows.Next() {
		var entry telemetry.LogEntry
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
