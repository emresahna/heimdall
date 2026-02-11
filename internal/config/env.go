package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServerAddr          string
	Port                string
	HTTPPort            string
	HTTPShutdownTimeout time.Duration
	ClickHouseConfig    ClickHouseConfig
	Agent               AgentConfig
}

type ClickHouseConfig struct {
	Addr     string
	User     string
	Password string
	DB       string
}

type AgentConfig struct {
	BatchSize       int
	FlushInterval   time.Duration
	MaxQueue        int
	K8sEnrich       bool
	HTTPSampleBytes int
	CorrelatorTTL   time.Duration
	NodeName        string
}

func Load() Config {
	nodeName := getEnv("NODE_NAME", "")
	if nodeName == "" {
		if host, err := os.Hostname(); err == nil {
			nodeName = host
		}
	}

	return Config{
		ServerAddr:          getEnv("SERVER_ADDR", ""),
		Port:                getEnv("PORT", "50051"),
		HTTPPort:            getEnv("HTTP_PORT", "8080"),
		HTTPShutdownTimeout: getEnvDuration("HTTP_SHUTDOWN_TIMEOUT", 5*time.Second),
		ClickHouseConfig: ClickHouseConfig{
			Addr:     getEnv("CLICKHOUSE_ADDR", "127.0.0.1:9000"),
			User:     getEnv("CLICKHOUSE_USER", "default"),
			Password: getEnv("CLICKHOUSE_PASSWORD", ""),
			DB:       getEnv("CLICKHOUSE_DB", "default"),
		},
		Agent: AgentConfig{
			BatchSize:       getEnvInt("AGENT_BATCH_SIZE", 200),
			FlushInterval:   getEnvDuration("AGENT_FLUSH_INTERVAL", 2*time.Second),
			MaxQueue:        getEnvInt("AGENT_MAX_QUEUE", 5000),
			K8sEnrich:       getEnvBool("AGENT_K8S_ENRICH", false),
			HTTPSampleBytes: getEnvInt("AGENT_HTTP_SAMPLE_BYTES", 128),
			CorrelatorTTL:   getEnvDuration("AGENT_CORRELATOR_TTL", 30*time.Second),
			NodeName:        nodeName,
		},
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}
