package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrMiss = errors.New("cache miss")

func (c *CacheClient) Get(ctx context.Context, key string) ([]byte, error) {
	cacheKey, err := c.buildKey(key)
	if err != nil {
		return nil, err
	}

	value, err := c.redis.Get(ctx, cacheKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrMiss
		}

		return nil, fmt.Errorf("cache: get %q: %w", cacheKey, err)
	}

	return value, nil
}

func (c *CacheClient) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	cacheKey, err := c.buildKey(key)
	if err != nil {
		return err
	}

	if ttl <= 0 {
		return fmt.Errorf("cache: TTL must be greater than zero")
	}
	if err := c.redis.Set(ctx, cacheKey, value, ttl).Err(); err != nil {
		return fmt.Errorf("cache: set %q: %w", cacheKey, err)
	}

	return nil
}

func (c *CacheClient) Delete(ctx context.Context, key string) error {
	cacheKey, err := c.buildKey(key)
	if err != nil {
		return err
	}

	if err := c.redis.Del(ctx, cacheKey).Err(); err != nil {
		return fmt.Errorf("cache: delete %q: %w", cacheKey, err)
	}

	return nil
}

func (c *CacheClient) GetDelete(ctx context.Context, key string) ([]byte, error) {
	cacheKey, err := c.buildKey(key)
	if err != nil {
		return nil, err
	}

	value, err := c.redis.GetDel(ctx, cacheKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrMiss
		}

		return nil, fmt.Errorf("cache: get and delete %q: %w", cacheKey, err)
	}

	return value, nil
}

func (c *CacheClient) buildKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("cache: key cannot be empty")
	}

	if c.keyPrefix == "" {
		return key, nil
	}

	return c.keyPrefix + ":" + key, nil
}
