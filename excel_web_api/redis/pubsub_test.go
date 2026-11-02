package redis_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"excel-libs/eventbus"
	redisclient "excel_web_api/redis"
)

type reconnectingBus struct {
	mu    sync.Mutex
	calls map[string]int
}

func (bus *reconnectingBus) Publish(context.Context, eventbus.Event) error {
	return nil
}

func (bus *reconnectingBus) Subscribe(
	_ context.Context,
	options eventbus.SubscribeOptions,
) (eventbus.Subscription, error) {
	bus.mu.Lock()
	bus.calls[options.Topic]++
	call := bus.calls[options.Topic]
	bus.mu.Unlock()
	if call == 1 {
		return disconnectedSubscription{}, nil
	}
	return blockingSubscription{}, nil
}

func (bus *reconnectingBus) callCount(topic string) int {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	return bus.calls[topic]
}

type disconnectedSubscription struct{}

func (disconnectedSubscription) Receive(context.Context) (eventbus.Delivery, error) {
	return eventbus.Delivery{}, errors.New("pubsub disconnected")
}

func (disconnectedSubscription) Ack(context.Context, eventbus.Delivery) error {
	return nil
}

func (disconnectedSubscription) Close() error {
	return nil
}

type blockingSubscription struct{}

func (blockingSubscription) Receive(ctx context.Context) (eventbus.Delivery, error) {
	<-ctx.Done()
	return eventbus.Delivery{}, ctx.Err()
}

func (blockingSubscription) Ack(context.Context, eventbus.Delivery) error {
	return nil
}

func (blockingSubscription) Close() error {
	return nil
}

func TestAuthInvalidatorWarnsAndReconnectsDisconnectedPubSub(t *testing.T) {
	logs := &lockedBuffer{}
	bus := &reconnectingBus{calls: make(map[string]int)}
	invalidator := redisclient.NewAuthInvalidator(
		redisclient.NewCache(nil),
		bus,
		slog.New(slog.NewTextHandler(logs, nil)),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := invalidator.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() { _ = invalidator.Close() })

	deadline := time.Now().Add(3 * time.Second)
	for {
		if bus.callCount("link.closed") >= 2 &&
			bus.callCount("share.changed") >= 2 &&
			strings.Contains(logs.String(), "level=WARN") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"reconnect state = link.closed:%d share.changed:%d logs:%q",
				bus.callCount("link.closed"),
				bus.callCount("share.changed"),
				logs.String(),
			)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(payload []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(payload)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}
