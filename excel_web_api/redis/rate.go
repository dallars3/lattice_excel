// redis/rate.go：IP 滑窗限速。
package redis

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const rateLimitKeyPrefix = "excel:rate:"

var slidingWindowScript = goredis.NewScript(`
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local window_ms = tonumber(ARGV[2])
local cutoff = now_ms - window_ms
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff)

local count = redis.call('ZCARD', KEYS[1])
local limit = tonumber(ARGV[1])
if count >= limit then
  local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  local retry_ms = window_ms
  if #oldest == 2 then
    retry_ms = tonumber(oldest[2]) + window_ms - now_ms
    if retry_ms < 1 then
      retry_ms = 1
    end
  end
  return {0, math.ceil(retry_ms / 1000), 0}
end

redis.call('ZADD', KEYS[1], now_ms, ARGV[3])
redis.call('PEXPIRE', KEYS[1], window_ms)
return {1, 0, limit - count - 1}
`)

// RateLimitResult is the admission decision for one sliding-window request.
type RateLimitResult struct {
	Allowed    bool
	Remaining  int64
	RetryAfter time.Duration
}

// RateLimiter stores fixed-capacity sliding windows in Redis.
type RateLimiter struct {
	client   *goredis.Client
	sequence atomic.Uint64
}

// NewRateLimiter creates a Redis-backed limiter.
func NewRateLimiter(client *goredis.Client) *RateLimiter {
	return &RateLimiter{client: client}
}

// RateLimiter returns the limiter bound to this cache's Redis connection.
func (cache *Cache) RateLimiter() *RateLimiter {
	if cache == nil || cache.client == nil {
		return nil
	}
	return NewRateLimiter(cache.client)
}

// RateLimitKey returns the Redis key for one scope and client identity.
func RateLimitKey(scope, identity string) string {
	return rateLimitKeyPrefix + strings.TrimSpace(scope) + ":" + strings.TrimSpace(identity)
}

// Allow admits at most limit requests in the trailing window.
func (limiter *RateLimiter) Allow(
	ctx context.Context,
	scope string,
	identity string,
	limit int64,
	window time.Duration,
) (RateLimitResult, error) {
	if limiter == nil || limiter.client == nil {
		return RateLimitResult{}, errors.New("rate limiter redis client is required")
	}
	if limit <= 0 {
		return RateLimitResult{}, errors.New("rate limit must be positive")
	}
	if window <= 0 {
		return RateLimitResult{}, errors.New("rate limit window must be positive")
	}
	member := strconv.FormatUint(limiter.sequence.Add(1), 10)
	values, err := slidingWindowScript.Run(
		ctx,
		limiter.client,
		[]string{RateLimitKey(scope, identity)},
		limit,
		window.Milliseconds(),
		member,
	).Slice()
	if err != nil {
		return RateLimitResult{}, err
	}
	if len(values) != 3 {
		return RateLimitResult{}, errors.New("rate limiter returned an invalid result")
	}
	allowed, allowedOK := values[0].(int64)
	retrySeconds, retryOK := values[1].(int64)
	remaining, remainingOK := values[2].(int64)
	if !allowedOK || !retryOK || !remainingOK {
		return RateLimitResult{}, errors.New("rate limiter returned non-integer values")
	}
	return RateLimitResult{
		Allowed:    allowed == 1,
		Remaining:  remaining,
		RetryAfter: time.Duration(retrySeconds) * time.Second,
	}, nil
}
