package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerConfig
	ClickHouseConfig
	CorrelatorConfig
	BatcherConfig
	CircuitBreakerConfig
	TLSConfig
}

type ServerConfig struct {
	NodeName            string        `env:"NODE_NAME"`
	ServerAddr          string        `env:"SERVER_ADDR"          default:"localhost:50051"`
	Port                string        `env:"PORT"                 default:"50051"`
	HTTPPort            string        `env:"HTTP_PORT"            default:"8080"`
	MetricsPort         string        `env:"METRICS_PORT"         default:"9090"`
	K8sEnrich           bool          `env:"K8S_ENRICH"           aliases:"AGENT_K8S_ENRICH"              default:"false"`
	DiagnosticsInterval time.Duration `env:"DIAGNOSTICS_INTERVAL" aliases:"AGENT_DIAGNOSTICS_INTERVAL"    default:"15s"`
	ShutdownTimeout     time.Duration `env:"SHUTDOWN_TIMEOUT"     aliases:"HTTP_SHUTDOWN_TIMEOUT"        default:"5s"`
	HTTPSampleBytes     int           `env:"HTTP_SAMPLE_BYTES"    aliases:"AGENT_HTTP_SAMPLE_BYTES"      default:"1024"`
}

type ClickHouseConfig struct {
	Addr             string        `env:"CLICKHOUSE_ADDR"               aliases:"CLICK_HOUSE_ADDR"               default:"127.0.0.1:9000"`
	User             string        `env:"CLICKHOUSE_USER"               aliases:"CLICK_HOUSE_USER"               default:"default"`
	Password         string        `env:"CLICKHOUSE_PASSWORD"           aliases:"CLICK_HOUSE_PASSWORD"           default:""`
	DB               string        `env:"CLICKHOUSE_DB"                 aliases:"CLICK_HOUSE_DB"                 default:"default"`
	MaxExecutionTime time.Duration `env:"CLICKHOUSE_MAX_EXECUTION_TIME" aliases:"CLICK_HOUSE_MAX_EXECUTION_TIME" default:"60s"`
	AsyncInsert      bool          `env:"CLICKHOUSE_ASYNC_INSERT"       aliases:"CLICK_HOUSE_ASYNC_INSERT"       default:"true"`
}

type CorrelatorConfig struct {
	TTL time.Duration `env:"CORRELATOR_TTL" aliases:"AGENT_CORRELATOR_TTL" default:"30s"`
}

type BatcherConfig struct {
	BatchSize     int           `env:"BATCHER_BATCH_SIZE"     aliases:"AGENT_BATCH_SIZE"       default:"200"`
	FlushInterval time.Duration `env:"BATCHER_FLUSH_INTERVAL" aliases:"AGENT_FLUSH_INTERVAL"   default:"2s"`
	MaxQueue      int           `env:"BATCHER_MAX_QUEUE"      aliases:"AGENT_MAX_QUEUE"        default:"1000"`
	RetryBackoff  time.Duration `env:"BATCHER_RETRY_BACKOFF"                                     default:"200ms"`
}

type CircuitBreakerConfig struct {
	CBThreshold    int           `env:"CB_THRESHOLD"     default:"5"`
	CBResetTimeout time.Duration `env:"CB_RESET_TIMEOUT" default:"30s"`
}

type TLSConfig struct {
	UseTLS      bool   `env:"USE_TLS"       default:"false"`
	TLSCertFile string `env:"TLS_CERT_FILE" default:""`
	TLSKeyFile  string `env:"TLS_KEY_FILE"  default:""`
	TLSCAFile   string `env:"TLS_CA_FILE"   default:""`
}

func loadEnvFile(path string) map[string]string {
	envKV := make(map[string]string)
	content, err := os.ReadFile(path)
	if err != nil {
		return envKV
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		envKV[key] = value
	}

	return envKV
}

func Load(p ...string) *Config {
	path := ".env"
	if len(p) > 0 {
		path = p[0]
	}

	envKV := loadEnvFile(path)

	var c Config
	if err := populateStruct(reflect.ValueOf(&c).Elem(), envKV); err != nil {
		panic(err)
	}
	if c.NodeName == "" {
		c.NodeName = hostnameOrFallback()
	}

	return &c
}

func hostnameOrFallback() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "localhost"
	}
	return host
}

func populateStruct(v reflect.Value, envKV map[string]string) error {
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		fieldV := v.Field(i)
		fieldT := t.Field(i)

		if fieldV.Kind() == reflect.Struct && fieldT.Anonymous {
			if err := populateStruct(fieldV, envKV); err != nil {
				return err
			}
			continue
		}

		if fieldV.Kind() == reflect.Struct {
			if err := populateStruct(fieldV, envKV); err != nil {
				return err
			}
			continue
		}

		envKeys := candidateEnvKeys(fieldT)
		val, _ := lookupEnvValue(envKV, envKeys)

		if val == "" {
			val = fieldT.Tag.Get("default")
		}

		if fieldT.Tag.Get("required") == "true" && val == "" {
			return fmt.Errorf("missing required environment variable: %s", envKeys[0])
		}

		if val == "" {
			continue
		}

		if err := setFieldValue(fieldV, val); err != nil {
			return fmt.Errorf("failed to set field %s: %w", fieldT.Name, err)
		}
	}

	return nil
}

func candidateEnvKeys(fieldT reflect.StructField) []string {
	envKey := fieldT.Tag.Get("env")
	if envKey == "" {
		envKey = strings.ToUpper(fieldT.Name)
	}

	keys := []string{envKey}
	if aliases := fieldT.Tag.Get("aliases"); aliases != "" {
		for _, alias := range strings.Split(aliases, ",") {
			alias = strings.TrimSpace(alias)
			if alias != "" {
				keys = append(keys, alias)
			}
		}
	}

	return keys
}

func lookupEnvValue(envKV map[string]string, envKeys []string) (string, bool) {
	for _, key := range envKeys {
		if val, ok := envKV[key]; ok {
			return val, true
		}
	}

	for _, key := range envKeys {
		if val := os.Getenv(key); val != "" {
			return val, true
		}
	}

	return "", false
}

func setFieldValue(field reflect.Value, val string) error {
	switch field.Interface().(type) {
	case time.Duration:
		d, err := time.ParseDuration(val)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(d))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
	case reflect.Int:
		i, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		field.SetInt(int64(i))
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		field.SetBool(b)
	default:
		return fmt.Errorf("unsupported type: %s", field.Kind())
	}

	return nil
}
