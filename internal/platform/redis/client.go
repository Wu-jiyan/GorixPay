// Package redisx provides the optional Redis dependency of the platform.
//
// A zero Config (empty Addr) means "Redis is not configured": Open returns a
// nil *Client and every method on a nil receiver degrades to a local no-op so
// callers never need a separate code path for "Redis absent".
package redisx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	defaultWindow = time.Minute
	dialTimeout   = 5 * time.Second
)

var ErrNotConfigured = errors.New("redis is not configured")

type Config struct {
	Addr     string
	Password string
	DB       int
	Prefix   string
}

type Client struct {
	client *goredis.Client
	prefix string
}

// Open connects to Redis. An empty Addr disables Redis and returns (nil, nil);
// a connection failure returns a descriptive error so the caller decides
// whether it is fatal.
func Open(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, nil
	}
	client := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect redis %q: %w", cfg.Addr, err)
	}
	return &Client{client: client, prefix: strings.TrimSpace(cfg.Prefix)}, nil
}

// Enabled reports whether a live Redis client is attached.
func (client *Client) Enabled() bool {
	return client != nil && client.client != nil
}

// Ping verifies connectivity; an unconfigured client is always healthy.
func (client *Client) Ping(ctx context.Context) error {
	if !client.Enabled() {
		return nil
	}
	return client.client.Ping(ctx).Err()
}

// Close releases the connection pool.
func (client *Client) Close() error {
	if !client.Enabled() {
		return nil
	}
	return client.client.Close()
}

// Once atomically claims key for ttl. It reports true when the caller is the
// first to claim it (SET NX PX), which is the replay-protection primitive.
// Without Redis every claim succeeds so single-node deployments keep working.
func (client *Client) Once(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if !client.Enabled() {
		return true, nil
	}
	if ttl <= 0 {
		ttl = defaultWindow
	}
	claimed, err := client.client.SetNX(ctx, client.key(key), "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("claim redis key: %w", err)
	}
	return claimed, nil
}

// Allow implements a fixed-window counter limiter: at most limit calls per
// window. Without Redis every call is allowed.
func (client *Client) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if !client.Enabled() {
		return true, nil
	}
	if limit <= 0 {
		return false, nil
	}
	if window <= 0 {
		window = defaultWindow
	}
	counter := client.key("rate:" + key)
	count, err := client.client.Incr(ctx, counter).Result()
	if err != nil {
		return false, fmt.Errorf("increment redis counter: %w", err)
	}
	if count == 1 {
		if err := client.client.Expire(ctx, counter, window).Err(); err != nil {
			return false, fmt.Errorf("expire redis counter: %w", err)
		}
	}
	return count <= int64(limit), nil
}

func (client *Client) key(name string) string {
	if client.prefix == "" {
		return name
	}
	return strings.TrimSuffix(client.prefix, ":") + ":" + name
}
