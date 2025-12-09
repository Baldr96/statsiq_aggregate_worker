package config

import (
	"fmt"
	"os"
)

// Config holds runtime configuration for the aggregate worker service.
type Config struct {
	DBURL      string
	RedisURL   string
	RedisQueue string
}

// Load builds a Config from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		DBURL:      os.Getenv("DB_URL"),
		RedisURL:   os.Getenv("REDIS_URL"),
		RedisQueue: os.Getenv("REDIS_QUEUE"),
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
