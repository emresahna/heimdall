package config

import (
	"os"
)

type Config struct {
	ServerAddr       string
	Port             string
	ClickHouseConfig ClickHouseConfig
}

type ClickHouseConfig struct {
	Addr     string
	User     string
	Password string
	DB       string
}

func Load() Config {
	return Config{
		ServerAddr: os.Getenv("SERVER_ADDR"),
		ClickHouseConfig: ClickHouseConfig{
			Addr:     os.Getenv("CLICKHOUSE_ADDR"),
			User:     os.Getenv("CLICKHOUSE_USER"),
			Password: os.Getenv("CLICKHOUSE_PASSWORD"),
			DB:       os.Getenv("CLICKHOUSE_DB"),
		},
		Port: os.Getenv("PORT"),
	}
}
