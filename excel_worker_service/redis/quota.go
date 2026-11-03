package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	goredis "github.com/redis/go-redis/v9"
)

const attachmentQuotaKey = "excel:file:attachment_pool_bytes"

var (
	// ErrQuotaExceeded reports that a reservation would exceed the pool limit.
	ErrQuotaExceeded = errors.New("attachment pool quota exceeded")

	reserveAttachmentBytes = goredis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local amount = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
if current + amount > limit then
  return -1
end
return redis.call("INCRBY", KEYS[1], amount)
`)
)

// AttachmentQuota owns the Redis attachment-pool reservation counter.
type AttachmentQuota struct {
	client *goredis.Client
	limit  int64
}

// NewAttachmentQuota creates the pool counter gateway.
func NewAttachmentQuota(client *goredis.Client, limit int64) *AttachmentQuota {
	return &AttachmentQuota{client: client, limit: limit}
}

// AttachmentQuotaKey returns the global attachment-pool counter key.
func AttachmentQuotaKey() string {
	return attachmentQuotaKey
}

// Limit returns the configured pool byte limit.
func (quota *AttachmentQuota) Limit() int64 {
	if quota == nil {
		return 0
	}
	return quota.limit
}

// Lookup reads the current reserved byte count.
func (quota *AttachmentQuota) Lookup(ctx context.Context) (int64, error) {
	if quota == nil || quota.client == nil {
		return 0, errors.New("attachment quota redis client is required")
	}
	value, err := quota.client.Get(ctx, attachmentQuotaKey).Result()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read attachment quota: %w", err)
	}
	used, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse attachment quota: %w", err)
	}
	return used, nil
}

// EnsureInitialized seeds an absent counter from the MySQL ledger.
func (quota *AttachmentQuota) EnsureInitialized(ctx context.Context, used int64) error {
	if quota == nil || quota.client == nil {
		return errors.New("attachment quota redis client is required")
	}
	initialize := goredis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local authoritative = tonumber(ARGV[1])
if authoritative > current then
  return redis.call("SET", KEYS[1], authoritative)
end
return current
`)
	if err := initialize.Run(
		ctx,
		quota.client,
		[]string{attachmentQuotaKey},
		used,
	).Err(); err != nil {
		return fmt.Errorf("initialize attachment quota: %w", err)
	}
	return nil
}

// Reserve atomically checks capacity and reserves bytes for one pending upload.
func (quota *AttachmentQuota) Reserve(ctx context.Context, size int64) error {
	if quota == nil || quota.client == nil {
		return errors.New("attachment quota redis client is required")
	}
	if size < 0 {
		return errors.New("attachment reservation size cannot be negative")
	}
	current, err := reserveAttachmentBytes.Run(
		ctx,
		quota.client,
		[]string{attachmentQuotaKey},
		size,
		quota.limit,
	).Int64()
	if err != nil {
		return fmt.Errorf("reserve attachment quota: %w", err)
	}
	if current < 0 {
		return ErrQuotaExceeded
	}
	return nil
}

// Release returns a failed or deleted upload reservation.
func (quota *AttachmentQuota) Release(ctx context.Context, size int64) error {
	if quota == nil || quota.client == nil {
		return errors.New("attachment quota redis client is required")
	}
	if size <= 0 {
		return nil
	}
	script := goredis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local amount = tonumber(ARGV[1])
if current <= amount then
  return redis.call("SET", KEYS[1], 0)
end
return redis.call("DECRBY", KEYS[1], amount)
`)
	if err := script.Run(
		ctx,
		quota.client,
		[]string{attachmentQuotaKey},
		size,
	).Err(); err != nil {
		return fmt.Errorf("release attachment quota: %w", err)
	}
	return nil
}

// Sync replaces the cache counter with the authoritative MySQL ledger.
func (quota *AttachmentQuota) Sync(ctx context.Context, used int64) error {
	if quota == nil || quota.client == nil {
		return errors.New("attachment quota redis client is required")
	}
	if used < 0 {
		used = 0
	}
	if err := quota.client.Set(ctx, attachmentQuotaKey, used, 0).Err(); err != nil {
		return fmt.Errorf("sync attachment quota: %w", err)
	}
	return nil
}
