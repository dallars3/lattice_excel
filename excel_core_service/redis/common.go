package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Open connects to Redis and verifies it with PING.
func Open(ctx context.Context, addr string) (*goredis.Client, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:         addr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	if err := Ping(ctx, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// Ping verifies Redis connectivity.
func Ping(ctx context.Context, client *goredis.Client) error {
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}
