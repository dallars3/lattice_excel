// redis/tracking.go：埋点事件短期幂等去重（SETNX，故障由调用方放行）。
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const trackingDedupeKeyPrefix = "excel:tracking:"

// TrackingDedupe stores short-lived per-event first-sighting claims.
type TrackingDedupe struct {
	client *goredis.Client
}

// NewTrackingDedupe creates a Redis-backed tracking dedupe.
func NewTrackingDedupe(client *goredis.Client) *TrackingDedupe {
	return &TrackingDedupe{client: client}
}

// Dedupe atomically claims eventID for the dedup window and reports whether
// this is the first sighting. Redis unavailability surfaces as error so the
// gateway can fail-open without dropping events（与限流 fail-open 同策略）。
func (dedupe *TrackingDedupe) Dedupe(
	ctx context.Context,
	eventID string,
	ttl time.Duration,
) (bool, error) {
	if dedupe == nil || dedupe.client == nil {
		return true, nil
	}
	first, err := dedupe.client.SetNX(
		ctx,
		trackingDedupeKeyPrefix+eventID,
		"1",
		ttl,
	).Result()
	if err != nil {
		return false, err
	}
	return first, nil
}