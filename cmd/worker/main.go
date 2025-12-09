package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"worker/internal/config"
	"worker/internal/db"
	"worker/internal/logging"
	"worker/internal/processor"
	queue "worker/internal/queue"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := logging.Logger()

	cfg, err := config.Load()
	if err != nil {
		logger.Errorf("config load failed: %v", err)
		os.Exit(1)
	}

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Errorf("db connection failed: %v", err)
		os.Exit(1)
	}
	defer pool.Close()

	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Errorf("invalid redis url: %v", err)
		os.Exit(1)
	}

	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()

	canonicalReader := db.NewCanonicalReader(pool)
	aggregateWriter := db.NewAggregateWriter(pool)

	proc := processor.NewAggregateProcessor(ctx, canonicalReader, aggregateWriter)
	q := queue.NewRedisQueue(redisClient)

	logger.Infof("aggregate worker started, consuming from queue: %s", cfg.RedisQueue)

	if err := q.Consume(ctx, cfg.RedisQueue, func(payload []byte) error {
		return proc.Handle(payload)
	}); err != nil && ctx.Err() == nil {
		logger.Errorf("queue consumption ended: %v", err)
		os.Exit(1)
	}
}
