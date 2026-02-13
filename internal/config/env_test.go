package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SERVER_ADDR", "")
	t.Setenv("PORT", "")
	t.Setenv("HTTP_PORT", "")

	cfg := Load()
	if cfg.Port == "" || cfg.HTTPPort == "" {
		t.Fatalf("expected defaults for ports")
	}
	if cfg.Agent.BatchSize <= 0 {
		t.Fatalf("expected default batch size")
	}
	if cfg.Agent.DiagnosticsInterval != 15*time.Second {
		t.Fatalf("expected default diagnostics interval")
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PORT", "6000")
	t.Setenv("HTTP_PORT", "9000")
	t.Setenv("AGENT_BATCH_SIZE", "10")
	t.Setenv("AGENT_DIAGNOSTICS_INTERVAL", "5s")

	cfg := Load()
	if cfg.Port != "6000" {
		t.Fatalf("expected PORT override")
	}
	if cfg.HTTPPort != "9000" {
		t.Fatalf("expected HTTP_PORT override")
	}
	if cfg.Agent.BatchSize != 10 {
		t.Fatalf("expected AGENT_BATCH_SIZE override")
	}
	if cfg.Agent.DiagnosticsInterval != 5*time.Second {
		t.Fatalf("expected AGENT_DIAGNOSTICS_INTERVAL override")
	}
}

func TestNodeNameFallback(t *testing.T) {
	t.Setenv("NODE_NAME", "")
	cfg := Load()
	if cfg.Agent.NodeName == "" {
		t.Fatalf("expected node name fallback")
	}
}

func TestServerAddrRequired(t *testing.T) {
	t.Setenv("SERVER_ADDR", "")
	cfg := Load()
	if cfg.ServerAddr != "" {
		t.Fatalf("expected empty server addr when not set")
	}
	os.Unsetenv("SERVER_ADDR")
}
