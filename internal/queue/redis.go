package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"worker/internal/logging"
)

const (
	defaultAggregateQueueKey = "aggregate_matches"
	retrySuffix              = ":retry"
	dlqSuffix                = ":dlq"
	retryCounterSuffix       = ":retry-count:"
	maxRetryAttempts         = 3
	brPopBlock               = 5 * time.Second
)

// RedisQueue implements queue operations using Redis lists.
type RedisQueue struct {
	client *redis.Client
	key    string
}

// NewRedisQueue builds a Redis-backed queue helper.
func NewRedisQueue(client *redis.Client) *RedisQueue {
	return &RedisQueue{client: client, key: defaultAggregateQueueKey}
}

// Consume uses BRPOP to deliver jobs to the handler until the context is canceled.
func (q *RedisQueue) Consume(ctx context.Context, queueName string, handler func([]byte) error) error {
	logger := logging.Logger()
	if queueName == "" {
		queueName = q.key
	}
	retryKey := queueName + retrySuffix
	dlqKey := queueName + dlqSuffix

	for {
		if ctx.Err() != nil {
			logger.Warnf("redis consumer exiting: %v", ctx.Err())
			return ctx.Err()
		}

		result, err := q.client.BRPop(ctx, brPopBlock, retryKey, queueName).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if ctx.Err() != nil {
				logger.Warnf("redis BRPOP canceled: %v", ctx.Err())
				return ctx.Err()
			}
			logger.Warnf("redis BRPOP error: %v", err)
			continue
		}
		if len(result) < 2 {
			continue
		}
		payload := []byte(result[1])
		if err := handler(payload); err != nil {
			logger.Warnf("handler error, scheduling retry: %v", err)
			if err := q.handleRetry(ctx, queueName, retryKey, dlqKey, payload); err != nil {
				logger.Errorf("retry handling failed: %v", err)
			}
			continue
		}
		_ = q.clearRetryCounter(ctx, queueName, payload)
	}
}

func (q *RedisQueue) handleRetry(ctx context.Context, baseQueue, retryKey, dlqKey string, payload []byte) error {
	logger := logging.Logger()
	attempt, err := q.incrementRetryCounter(ctx, baseQueue, payload)
	if err != nil {
		return err
	}
	if attempt > maxRetryAttempts {
		logger.Warnf("moving job to DLQ after %d attempts", attempt-1)
		_ = q.client.LPush(ctx, dlqKey, payload).Err()
		_ = q.clearRetryCounter(ctx, baseQueue, payload)
		return nil
	}
	return q.client.LPush(ctx, retryKey, payload).Err()
}

func (q *RedisQueue) incrementRetryCounter(ctx context.Context, queueName string, payload []byte) (int64, error) {
	key := retryCounterKey(queueName, payload)
	count, err := q.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	_ = q.client.Expire(ctx, key, 24*time.Hour).Err()
	return count, nil
}

func (q *RedisQueue) clearRetryCounter(ctx context.Context, queueName string, payload []byte) error {
	key := retryCounterKey(queueName, payload)
	return q.client.Del(ctx, key).Err()
}

func retryCounterKey(queue string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%s%s%s", queue, retryCounterSuffix, hex.EncodeToString(sum[:]))
}
