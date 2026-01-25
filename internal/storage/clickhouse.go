package storage

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type LogEntry struct {
	Timestamp  time.Time `ch:"timestamp"`
	Pid        uint32    `ch:"pid"`
	Type       string    `ch:"type"`
	Payload    string    `ch:"payload"`
	DurationNs uint64    `ch:"duration_ns"`
}

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
		Addr: []string{cfg.Addr},
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
		timestamp DateTime,
		pid UInt32,
		type String,
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
			log.Payload,
			log.DurationNs,
		)
		if err != nil {
			return err
		}
	}

	return batch.Send()
}
