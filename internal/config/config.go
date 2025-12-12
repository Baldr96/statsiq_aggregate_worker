package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds runtime configuration for the aggregate worker service.
type Config struct {
	DBURL         string
	RedisURL      string
	RedisQueue    string
	WorkerCount   int
	JobBufferSize int
}

// Load builds a Config from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		DBURL:         os.Getenv("DB_URL"),
		RedisURL:      os.Getenv("REDIS_URL"),
		RedisQueue:    os.Getenv("REDIS_QUEUE"),
		WorkerCount:   getEnvInt("WORKER_COUNT", 4),
		JobBufferSize: getEnvInt("JOB_BUFFER_SIZE", 100),
	}

	if cfg.DBURL == "" {
		return nil, fmt.Errorf("DB_URL is required")
	}

	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	if cfg.RedisQueue == "" {
		cfg.RedisQueue = "aggregate_matches"
	}

	return cfg, nil
}

// getEnvInt returns an environment variable as int or a default value.
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}
