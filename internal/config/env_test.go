package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NODE_NAME", "")
	t.Setenv("PORT", "")
	t.Setenv("DIAGNOSTICS_INTERVAL", "")
	t.Setenv("SERVER_ADDR", "")
	t.Setenv("CLICKHOUSE_ADDR", "")

	cfg := Load("non-existent-file")

	if cfg.NodeName == "" {
		t.Fatal("expected hostname fallback for NodeName")
	}
	if cfg.ServerAddr != "localhost:50051" {
		t.Errorf("expected default ServerAddr 'localhost:50051', got %q", cfg.ServerAddr)
	}
	if cfg.Port != "50051" {
		t.Errorf("expected default Port '50051', got '%s'", cfg.Port)
	}
	if cfg.DiagnosticsInterval != 15*time.Second {
		t.Errorf("expected default DiagnosticsInterval 15s, got %v", cfg.DiagnosticsInterval)
	}
	if cfg.Addr != "127.0.0.1:9000" {
		t.Errorf("expected default ClickHouse addr, got %q", cfg.Addr)
	}
	if cfg.HTTPSampleBytes != 0 {
		t.Errorf("expected default HTTPSampleBytes 0 (off), got %d", cfg.HTTPSampleBytes)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("NODE_NAME", "test-node")
	t.Setenv("PORT", "9090")
	t.Setenv("DIAGNOSTICS_INTERVAL", "30s")
	t.Setenv("BATCHER_BATCH_SIZE", "500")

	cfg := Load("non-existent-file")

	if cfg.NodeName != "test-node" {
		t.Errorf("expected NodeName 'test-node', got '%s'", cfg.NodeName)
	}
	if cfg.Port != "9090" {
		t.Errorf("expected Port '9090', got '%s'", cfg.Port)
	}
	if cfg.DiagnosticsInterval != 30*time.Second {
		t.Errorf("expected DiagnosticsInterval 30s, got %v", cfg.DiagnosticsInterval)
	}
	if cfg.BatchSize != 500 {
		t.Errorf("expected BatchSize 500, got %d", cfg.BatchSize)
	}
}

func TestLoadLegacyEnvAliases(t *testing.T) {
	t.Setenv("CLICK_HOUSE_ADDR", "legacy-clickhouse:9000")
	t.Setenv("AGENT_BATCH_SIZE", "321")
	t.Setenv("AGENT_DIAGNOSTICS_INTERVAL", "45s")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "9s")

	cfg := Load("non-existent-file")

	if cfg.Addr != "legacy-clickhouse:9000" {
		t.Errorf("expected legacy ClickHouse alias to load, got %q", cfg.Addr)
	}
	if cfg.BatchSize != 321 {
		t.Errorf("expected legacy batch size alias to load, got %d", cfg.BatchSize)
	}
	if cfg.DiagnosticsInterval != 45*time.Second {
		t.Errorf("expected legacy diagnostics alias to load, got %v", cfg.DiagnosticsInterval)
	}
	if cfg.ShutdownTimeout != 9*time.Second {
		t.Errorf("expected legacy shutdown alias to load, got %v", cfg.ShutdownTimeout)
	}
}

func TestLoadFile(t *testing.T) {
	content := `
NODE_NAME=file-node
PORT=7070
CLICKHOUSE_ADDR=clickhouse-from-file:9000
BATCHER_FLUSH_INTERVAL=5s
`
	tmpFile := "test.env"
	if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile)

	// File should override defaults
	cfg := Load(tmpFile)

	if cfg.NodeName != "file-node" {
		t.Errorf("expected NodeName 'file-node', got '%s'", cfg.NodeName)
	}
	if cfg.Port != "7070" {
		t.Errorf("expected Port '7070', got '%s'", cfg.Port)
	}
	if cfg.Addr != "clickhouse-from-file:9000" {
		t.Errorf("expected ClickHouse addr from file, got %q", cfg.Addr)
	}
	if cfg.FlushInterval != 5*time.Second {
		t.Errorf("expected FlushInterval 5s, got %v", cfg.FlushInterval)
	}
}

func TestTypeParsing(t *testing.T) {
	t.Setenv("K8S_ENRICH", "true")
	t.Setenv("BATCHER_BATCH_SIZE", "123")
	t.Setenv("CORRELATOR_TTL", "1m")

	cfg := Load("non-existent-file")

	if cfg.K8sEnrich != true {
		t.Errorf("expected K8sEnrich true, got %v", cfg.K8sEnrich)
	}
	if cfg.BatchSize != 123 {
		t.Errorf("expected BatchSize 123, got %d", cfg.BatchSize)
	}
	if cfg.TTL != time.Minute {
		t.Errorf("expected TTL 1m, got %v", cfg.TTL)
	}
}
