package bus_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"excel-libs/eventbus"
	"excel_core_service/base"
	"excel_core_service/bus"
)

func TestConsumerUsesExponentialBackoffAndExposesDeadLetter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := openCoreDB(t)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	broker := eventbus.NewMemoryBus()
	eventType := fmt.Sprintf("test.retry.%d", time.Now().UnixNano())
	eventID := fmt.Sprintf("evt_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_bus_outbox WHERE event_id = ?`, eventID)
	})

	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       eventType,
			Version:    1,
			BaseID:     "base_retry",
			Seq:        9,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_core_service.comment",
		},
		Payload: json.RawMessage(`{"comment_id":"cmt_retry"}`),
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if err := outbox.Enqueue(ctx, tx, event); err != nil {
		_ = tx.Rollback()
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	policy := eventbus.RetryPolicy{
		Initial:     10 * time.Millisecond,
		Max:         20 * time.Millisecond,
		MaxAttempts: 3,
	}
	wantDelays := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond}
	for index, want := range wantDelays {
		if got := policy.Delay(index + 1); got != want {
			t.Fatalf("Delay(%d) = %s, want %s", index+1, got, want)
		}
	}

	consumer := bus.NewConsumer(store, broker, bus.ConsumerConfig{Retry: policy})
	var attempts atomic.Int32
	if err := consumer.Register(eventbus.SubscribeOptions{
		Topic:    eventType,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    "core-test",
		Consumer: "core-test",
	}, func(context.Context, eventbus.Event) error {
		attempts.Add(1)
		return errors.New("handler unavailable")
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = consumer.Stop(stopCtx)
	})

	if err := broker.Publish(ctx, event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	deadLetter := waitForDeadLetter(t, ctx, outbox, eventID)
	if got := attempts.Load(); got != 3 {
		t.Fatalf("handler attempts = %d, want 3", got)
	}
	if deadLetter.Attempts != 3 {
		t.Fatalf("dead letter attempts = %d, want 3", deadLetter.Attempts)
	}
	if deadLetter.LastError != "handler unavailable" {
		t.Fatalf("dead letter last_error = %q, want handler unavailable", deadLetter.LastError)
	}
}

func waitForDeadLetter(
	t *testing.T,
	ctx context.Context,
	outbox *bus.Outbox,
	eventID string,
) base.DeadLetter {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		letters, err := outbox.DeadLetters(ctx, 20)
		if err != nil {
			t.Fatalf("DeadLetters() error = %v", err)
		}
		for _, letter := range letters {
			if letter.EventID == eventID {
				return letter
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for dead letter: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("dead letter for %s not visible", eventID)
	return base.DeadLetter{}
}
