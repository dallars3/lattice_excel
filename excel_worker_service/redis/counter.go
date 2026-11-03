// redis/counter.go：站内通知未读计数。
package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

const notifyUnreadPrefix = "excel:notify:unread:"

var decrementNotifyUnread = goredis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
if current <= 0 then
  return 0
end
return redis.call("DECR", KEYS[1])
`)

// NotifyCounters owns the Redis unread projections.
type NotifyCounters struct {
	client *goredis.Client
}

// NewNotifyCounters creates the notification unread counter gateway.
func NewNotifyCounters(client *goredis.Client) *NotifyCounters {
	return &NotifyCounters{client: client}
}

// NotifyUnreadKey returns the per-user unread cache key.
func NotifyUnreadKey(userID string) string {
	return notifyUnreadPrefix + strings.TrimSpace(userID)
}

// Lookup reads the cached count and reports whether the key exists.
func (counters *NotifyCounters) Lookup(
	ctx context.Context,
	userID string,
) (int64, bool, error) {
	if counters == nil || counters.client == nil {
		return 0, false, errors.New("notify redis client is required")
	}
	if strings.TrimSpace(userID) == "" {
		return 0, false, errors.New("notify counter user_id is required")
	}
	count, err := counters.client.Get(ctx, NotifyUnreadKey(userID)).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read notify unread count: %w", err)
	}
	return count, true, nil
}

// Increment atomically increments one user's unread count.
func (counters *NotifyCounters) Increment(ctx context.Context, userID string) error {
	if counters == nil || counters.client == nil {
		return errors.New("notify redis client is required")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("notify counter user_id is required")
	}
	if err := counters.client.Incr(ctx, NotifyUnreadKey(userID)).Err(); err != nil {
		return fmt.Errorf("increment notify unread count: %w", err)
	}
	return nil
}

// Decrement atomically decrements one user's unread count without going below zero.
func (counters *NotifyCounters) Decrement(ctx context.Context, userID string) error {
	if counters == nil || counters.client == nil {
		return errors.New("notify redis client is required")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("notify counter user_id is required")
	}
	if err := decrementNotifyUnread.Run(
		ctx,
		counters.client,
		[]string{NotifyUnreadKey(userID)},
	).Err(); err != nil {
		return fmt.Errorf("decrement notify unread count: %w", err)
	}
	return nil
}

// Set overwrites the cached unread projection during self-healing reads.
func (counters *NotifyCounters) Set(ctx context.Context, userID string, count int64) error {
	if counters == nil || counters.client == nil {
		return errors.New("notify redis client is required")
	}
	if count < 0 {
		count = 0
	}
	if err := counters.client.Set(
		ctx,
		NotifyUnreadKey(userID),
		count,
		0,
	).Err(); err != nil {
		return fmt.Errorf("set notify unread count: %w", err)
	}
	return nil
}

// Clear removes one user's unread projection after mark-all-read.
func (counters *NotifyCounters) Clear(ctx context.Context, userID string) error {
	if counters == nil || counters.client == nil {
		return errors.New("notify redis client is required")
	}
	if err := counters.client.Del(ctx, NotifyUnreadKey(userID)).Err(); err != nil {
		return fmt.Errorf("clear notify unread count: %w", err)
	}
	return nil
}
