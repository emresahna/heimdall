package storage

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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

type LogEntry struct {
	Timestamp  time.Time
	Pid        uint32
	Type       string
	Status     uint32
	Method     string
	Path       string
	Payload    string // Kept for retro-compatibility
	DurationNs uint64
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
	// Dropping old table for simplicity in this refactor since schema changed drastically
	// In production, we would use ALTER TABLE or versioned migrations.
	_ = db.conn.Exec(context.Background(), "DROP TABLE IF EXISTS http_logs")

	schema := `
	CREATE TABLE IF NOT EXISTS http_logs (
		timestamp DateTime64(9),
		pid UInt32,
		type String,
		status UInt32,
		method String,
		path String,
		payload String,
		duration_ns UInt64
	) ENGINE = MergeTree()
	ORDER BY timestamp
	`
	return db.conn.Exec(context.Background(), schema)
}

func (db *DB) InsertBatch(logs []LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	ctx := context.Background()

	batch, err := db.conn.PrepareBatch(ctx, "INSERT INTO http_logs")
	if err != nil {
		return err
	}

	for _, log := range logs {
		err := batch.Append(
			log.Timestamp,
			log.Pid,
			log.Type,
			log.Status,
			log.Method,
			log.Path,
			log.Payload,
			log.DurationNs,
		)
		if err != nil {
			return err
		}
	}

	return batch.Send()
}
