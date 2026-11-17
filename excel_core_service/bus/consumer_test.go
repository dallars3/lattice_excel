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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	deadline := time.Now().Add(8 * time.Second)
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

// TestConsumerKeepsPollingAfterIdleDeadline 锁：Redis Streams 在阻塞窗口内无
// 新事件时返回 DeadlineExceeded，消费循环必须继续等待下一条；否则首次空闲后
// 循环永久退出，import.finished 等事件永远不会被消费。
func TestConsumerKeepsPollingAfterIdleDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db := openCoreDB(t)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	eventType := fmt.Sprintf("test.idle.%d", time.Now().UnixNano())
	eventID := fmt.Sprintf("evt_idle_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_bus_outbox WHERE event_id = ?`, eventID)
	})
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       eventType,
			Version:    1,
			BaseID:     "base_idle",
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_core_service.test",
		},
		Payload: json.RawMessage(`{"base_id":"base_idle"}`),
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

	consumer := bus.NewConsumer(store, &idleThenEventBus{event: event}, bus.ConsumerConfig{})
	var handled atomic.Int32
	if err := consumer.Register(eventbus.SubscribeOptions{
		Topic:    eventType,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    "core-idle-test",
		Consumer: "core-idle-test",
	}, func(context.Context, eventbus.Event) error {
		handled.Add(1)
		return nil
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

	deadline := time.Now().Add(5 * time.Second)
	for handled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := handled.Load(); got != 1 {
		t.Fatalf("handler calls after idle DeadlineExceeded = %d, want 1", got)
	}
}

// TestConsumerProcessesExternalEventsWithoutOutboxRow 锁：worker 直接发布到
// Redis 的事件（如 import.finished）没有 core outbox 行，必须经
// RegisterExternal 直接调用处理器，否则事件会被静默 ack 而业务永远不生效。
func TestConsumerProcessesExternalEventsWithoutOutboxRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db := openCoreDB(t)
	store := base.NewBusStore(db)
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    fmt.Sprintf("evt_ext_%d", time.Now().UnixNano()),
			Type:       fmt.Sprintf("test.external.%d", time.Now().UnixNano()),
			Version:    1,
			BaseID:     "base_external",
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_worker_service.test",
		},
		Payload: json.RawMessage(`{"base_id":"base_external"}`),
	}

	consumer := bus.NewConsumer(store, &idleThenEventBus{event: event}, bus.ConsumerConfig{})
	var handled atomic.Int32
	if err := consumer.RegisterExternal(eventbus.SubscribeOptions{
		Topic:    event.Header.Type,
		Mode:     eventbus.DeliveryModeReliable,
		Group:    "core-external-test",
		Consumer: "core-external-test",
	}, func(context.Context, eventbus.Event) error {
		handled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("RegisterExternal() error = %v", err)
	}
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = consumer.Stop(stopCtx)
	})

	deadline := time.Now().Add(5 * time.Second)
	for handled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := handled.Load(); got != 1 {
		t.Fatalf("external handler calls = %d, want 1", got)
	}
}

// idleThenEventBus 先让订阅空闲一次（DeadlineExceeded），再投递一条事件，
// 之后阻塞到 context 结束，用于验证消费循环不会在空闲后退化。
type idleThenEventBus struct {
	event eventbus.Event
}

func (bus *idleThenEventBus) Publish(context.Context, eventbus.Event) error { return nil }

func (bus *idleThenEventBus) Subscribe(
	context.Context,
	eventbus.SubscribeOptions,
) (eventbus.Subscription, error) {
	return &idleThenSubscription{bus: bus}, nil
}

type idleThenSubscription struct {
	bus       *idleThenEventBus
	idleSent  bool
	eventSent bool
}

func (subscription *idleThenSubscription) Receive(
	ctx context.Context,
) (eventbus.Delivery, error) {
	if !subscription.idleSent {
		subscription.idleSent = true
		return eventbus.Delivery{}, context.DeadlineExceeded
	}
	if !subscription.eventSent {
		subscription.eventSent = true
		return eventbus.Delivery{Event: subscription.bus.event}, nil
	}
	<-ctx.Done()
	return eventbus.Delivery{}, ctx.Err()
}

func (subscription *idleThenSubscription) Ack(context.Context, eventbus.Delivery) error {
	return nil
}

func (subscription *idleThenSubscription) Close() error { return nil }
