package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/user/nexus/config"
)

// NewClient parses REDIS_URL and returns a connected Redis client.
func NewClient(cfg *config.Config) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
