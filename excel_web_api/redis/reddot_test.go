package redis_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"excel-libs/eventbus"
	webredis "excel_web_api/redis"
)

func TestRedDotObserverPublishesNotificationCreatedByUser(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	received := make(chan webredis.RedDotEvent, 1)
	observer := webredis.NewRedDotObserver(
		bus,
		func(_ context.Context, event webredis.RedDotEvent) error {
			received <- event
			return nil
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := observer.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = observer.Close() })

	payload, err := json.Marshal(map[string]any{
		"notification_id": "notification-1",
		"user_id":         "user-1",
		"type":            "comment.mentioned",
		"base_id":         "base-1",
	})
	if err != nil {
		t.Fatalf("marshal notify.created payload: %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "notify-created-1",
			Type:       "notify.created",
			Version:    1,
			BaseID:     "base-1",
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_worker_service.notify",
		},
		Payload: payload,
	}); err != nil {
		t.Fatalf("Publish(notify.created) error = %v", err)
	}

	select {
	case event := <-received:
		if event.UserID != "user-1" ||
			event.NotificationID != "notification-1" ||
			event.Type != "comment.mentioned" ||
			event.BaseID != "base-1" {
			t.Fatalf("red-dot event = %+v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for red-dot event")
	}
}
