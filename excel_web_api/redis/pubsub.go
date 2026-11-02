// redis/pubsub.go：鉴权缓存失效订阅与断线重连。
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"excel-libs/eventbus"
)

const invalidatorReconnectDelay = time.Second

var authInvalidationTopics = []string{"link.closed", "share.changed"}

// AuthInvalidator subscribes to share invalidation events and evicts all
// cached decisions for the affected Base.
type AuthInvalidator struct {
	cache  *Cache
	bus    eventbus.EventBus
	logger *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
	wait    sync.WaitGroup
	active  map[eventbus.Subscription]struct{}
	started bool
	closed  bool
}

// NewAuthInvalidator creates the gateway authorization-cache subscriber.
func NewAuthInvalidator(
	cache *Cache,
	bus eventbus.EventBus,
	logger *slog.Logger,
) *AuthInvalidator {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthInvalidator{
		cache:  cache,
		bus:    bus,
		logger: logger,
		active: make(map[eventbus.Subscription]struct{}),
	}
}

// Start subscribes to link.closed and share.changed. Receive failures trigger
// WARNING logs and periodic resubscription until Close or context cancellation.
func (invalidator *AuthInvalidator) Start(ctx context.Context) error {
	invalidator.mu.Lock()
	defer invalidator.mu.Unlock()
	if invalidator.started {
		return errors.New("auth invalidator already started")
	}
	if invalidator.closed {
		return errors.New("auth invalidator is closed")
	}
	if invalidator.bus == nil {
		return errors.New("event bus is required")
	}
	if invalidator.cache == nil {
		return errors.New("cache is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	invalidator.started = true
	invalidator.cancel = cancel
	for _, topic := range authInvalidationTopics {
		invalidator.wait.Add(1)
		go invalidator.consume(runCtx, topic)
	}
	return nil
}

// Close stops subscriptions and waits for their workers.
func (invalidator *AuthInvalidator) Close() error {
	invalidator.mu.Lock()
	if invalidator.closed {
		invalidator.mu.Unlock()
		return nil
	}
	invalidator.closed = true
	cancel := invalidator.cancel
	active := make([]eventbus.Subscription, 0, len(invalidator.active))
	for subscription := range invalidator.active {
		active = append(active, subscription)
	}
	invalidator.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, subscription := range active {
		_ = subscription.Close()
	}
	invalidator.wait.Wait()
	return nil
}

func (invalidator *AuthInvalidator) subscribe(
	ctx context.Context,
	topic string,
) (eventbus.Subscription, error) {
	return invalidator.bus.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: topic,
		Mode:  eventbus.DeliveryModePubSub,
	})
}

func (invalidator *AuthInvalidator) consume(
	ctx context.Context,
	topic string,
) {
	defer invalidator.wait.Done()
	var current eventbus.Subscription
	defer func() {
		if current != nil {
			invalidator.untrack(current)
		}
	}()
	for {
		if current == nil {
			subscription, err := invalidator.subscribe(ctx, topic)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				invalidator.logger.Warn(
					"authorization invalidation subscribe failed; retrying",
					"topic",
					topic,
					"error",
					err,
				)
				if !invalidator.waitForReconnect(ctx) {
					return
				}
				continue
			}
			current = subscription
			invalidator.track(current)
			if ctx.Err() != nil {
				invalidator.untrack(current)
				_ = current.Close()
				return
			}
		}
		delivery, err := current.Receive(ctx)
		if err == nil {
			invalidator.invalidate(ctx, delivery.Event)
			continue
		}
		invalidator.untrack(current)
		_ = current.Close()
		current = nil
		if ctx.Err() != nil {
			return
		}
		invalidator.logger.Warn(
			"authorization invalidation subscription disconnected; reconnecting",
			"topic",
			topic,
			"error",
			err,
		)
		if !invalidator.waitForReconnect(ctx) {
			return
		}
	}
}

func (invalidator *AuthInvalidator) waitForReconnect(ctx context.Context) bool {
	timer := time.NewTimer(invalidatorReconnectDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (invalidator *AuthInvalidator) track(subscription eventbus.Subscription) {
	invalidator.mu.Lock()
	invalidator.active[subscription] = struct{}{}
	invalidator.mu.Unlock()
}

func (invalidator *AuthInvalidator) untrack(subscription eventbus.Subscription) {
	invalidator.mu.Lock()
	delete(invalidator.active, subscription)
	invalidator.mu.Unlock()
}

func (invalidator *AuthInvalidator) invalidate(ctx context.Context, event eventbus.Event) {
	baseID := event.Header.BaseID
	if baseID == "" {
		var payload struct {
			BaseID string `json:"base_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err == nil {
			baseID = payload.BaseID
		}
	}
	if baseID == "" {
		invalidator.logger.Warn(
			"authorization invalidation event has no base_id",
			"topic",
			event.Header.Type,
			"event_id",
			event.Header.EventID,
		)
		return
	}
	if _, err := invalidator.cache.InvalidateAccessByBase(ctx, baseID); err != nil {
		invalidator.logger.Warn(
			"authorization cache invalidation failed",
			"base_id",
			baseID,
			"error",
			err,
		)
	}
}
