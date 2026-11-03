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

const redDotReconnectDelay = time.Second

// RedDotEvent is the gateway-facing projection of notify.created.
type RedDotEvent struct {
	NotificationID string
	UserID         string
	Type           string
	BaseID         string
}

// RedDotHandler is the upper-layer push seam. It receives the target user id so
// the realtime adapter can address the relevant connection without depending on
// a concrete WS implementation.
type RedDotHandler func(context.Context, RedDotEvent) error

// RedDotObserver subscribes to notify.created and forwards each event to the
// configured framing-agnostic handler.
type RedDotObserver struct {
	bus     eventbus.EventBus
	handler RedDotHandler
	logger  *slog.Logger

	mu           sync.Mutex
	started      bool
	closed       bool
	cancel       context.CancelFunc
	subscription eventbus.Subscription
	wait         sync.WaitGroup
}

// NewRedDotObserver creates an observer for notify.created PubSub events.
func NewRedDotObserver(
	bus eventbus.EventBus,
	handler RedDotHandler,
	loggers ...*slog.Logger,
) *RedDotObserver {
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &RedDotObserver{
		bus:     bus,
		handler: handler,
		logger:  logger,
	}
}

// Start subscribes to transient notify.created events.
func (observer *RedDotObserver) Start(ctx context.Context) error {
	if observer == nil || observer.bus == nil {
		return errors.New("red-dot event bus is required")
	}
	if observer.handler == nil {
		return errors.New("red-dot handler is required")
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.started {
		return errors.New("red-dot observer already started")
	}
	if observer.closed {
		return errors.New("red-dot observer is closed")
	}
	runCtx, cancel := context.WithCancel(ctx)
	subscription, err := observer.bus.Subscribe(runCtx, eventbus.SubscribeOptions{
		Topic: "notify.created",
		Mode:  eventbus.DeliveryModePubSub,
	})
	if err != nil {
		cancel()
		return err
	}
	observer.cancel = cancel
	observer.subscription = subscription
	observer.started = true
	observer.wait.Add(1)
	go observer.consume(runCtx, subscription)
	return nil
}

// Handle projects one notify.created event for the handler.
func (observer *RedDotObserver) Handle(
	ctx context.Context,
	event eventbus.Event,
) error {
	if event.Header.Type != "notify.created" {
		return errors.New("red-dot observer only handles notify.created")
	}
	var payload struct {
		NotificationID string `json:"notification_id"`
		UserID         string `json:"user_id"`
		Type           string `json:"type"`
		BaseID         string `json:"base_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	if payload.NotificationID == "" || payload.UserID == "" || payload.Type == "" {
		return errors.New("notify.created payload is incomplete")
	}
	baseID := payload.BaseID
	if baseID == "" {
		baseID = event.Header.BaseID
	}
	if observer == nil || observer.handler == nil {
		return errors.New("red-dot handler is required")
	}
	return observer.handler(ctx, RedDotEvent{
		NotificationID: payload.NotificationID,
		UserID:         payload.UserID,
		Type:           payload.Type,
		BaseID:         baseID,
	})
}

// Close cancels the subscription and waits for the receive loop.
func (observer *RedDotObserver) Close() error {
	if observer == nil {
		return nil
	}
	observer.mu.Lock()
	if observer.closed {
		observer.mu.Unlock()
		return nil
	}
	observer.closed = true
	cancel := observer.cancel
	subscription := observer.subscription
	observer.cancel = nil
	observer.subscription = nil
	observer.started = false
	observer.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if subscription != nil {
		_ = subscription.Close()
	}
	observer.wait.Wait()
	return nil
}

func (observer *RedDotObserver) consume(
	ctx context.Context,
	current eventbus.Subscription,
) {
	defer observer.wait.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		if current == nil {
			subscription, err := observer.bus.Subscribe(ctx, eventbus.SubscribeOptions{
				Topic: "notify.created",
				Mode:  eventbus.DeliveryModePubSub,
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				observer.logger.Warn("red-dot subscribe failed; retrying", "error", err)
				if !waitForRedDotReconnect(ctx) {
					return
				}
				continue
			}
			current = subscription
			observer.mu.Lock()
			if observer.closed {
				observer.mu.Unlock()
				_ = current.Close()
				return
			}
			observer.subscription = current
			observer.mu.Unlock()
		}

		for {
			delivery, err := current.Receive(ctx)
			if err == nil {
				if handleErr := observer.Handle(ctx, delivery.Event); handleErr != nil {
					observer.logger.Warn(
						"red-dot notification dropped",
						"event_id", delivery.Event.Header.EventID,
						"error", handleErr,
					)
				}
				continue
			}
			observer.mu.Lock()
			if observer.subscription == current {
				observer.subscription = nil
			}
			observer.mu.Unlock()
			_ = current.Close()
			current = nil
			if ctx.Err() != nil {
				return
			}
			observer.logger.Warn("red-dot subscription disconnected; reconnecting", "error", err)
			break
		}
		if !waitForRedDotReconnect(ctx) {
			return
		}
	}
}

func waitForRedDotReconnect(ctx context.Context) bool {
	timer := time.NewTimer(redDotReconnectDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
