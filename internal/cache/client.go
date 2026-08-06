package cache

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	URL       string
	KeyPrefix string
}

type CacheClient struct {
	redis     *redis.Client
	keyPrefix string
}

func Open(ctx context.Context, cfg Config) (*CacheClient, error) {
	rawURL := strings.TrimSpace(cfg.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("cache: URL is required")
	}

	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("cache: parse URL: %w", err)
	}

	redisClient := redis.NewClient(options)

	if err := redisClient.Ping(ctx).Err(); err != nil {
		_ = redisClient.Close()
		return nil, fmt.Errorf("cache: ping server: %w", err)
	}

	return &CacheClient{
		redis:     redisClient,
		keyPrefix: strings.TrimSpace(cfg.KeyPrefix),
	}, nil
}

func (c *CacheClient) Close() error {
	if err := c.redis.Close(); err != nil {
		return fmt.Errorf("cache: close client: %w", err)
	}

	return nil
}
