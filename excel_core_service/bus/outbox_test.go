package bus_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"excel-libs/eventbus"
	"excel_core_service/base"
	"excel_core_service/bus"
)

func TestOutboxPublishesOnlyAfterBusinessTransactionCommits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := openCoreDB(t)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	broker := eventbus.NewMemoryBus()
	eventType := fmt.Sprintf("test.outbox.%d", time.Now().UnixNano())

	subscription, err := broker.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: eventType,
		Mode:  eventbus.DeliveryModeReliable,
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()

	eventID := fmt.Sprintf("evt_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM excel_bus_outbox WHERE event_id = ?`, eventID)
	})
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       eventType,
			Version:    1,
			BaseID:     "base_test",
			Seq:        42,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "excel_core_service.records",
		},
		Payload: json.RawMessage(`{"record_id":"rec_test","value":"committed"}`),
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if err := outbox.Enqueue(ctx, tx, event); err != nil {
		_ = tx.Rollback()
		t.Fatalf("Enqueue() error = %v", err)
	}

	forwarder := bus.NewForwarder(store, broker, bus.ForwarderConfig{BatchSize: 10})
	if _, err := forwarder.ForwardOnce(ctx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ForwardOnce() before commit error = %v", err)
	}
	assertNoDelivery(t, subscription)

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if _, err := forwarder.ForwardOnce(ctx); err != nil {
		t.Fatalf("ForwardOnce() after commit error = %v", err)
	}

	receiveCtx, receiveCancel := context.WithTimeout(ctx, time.Second)
	defer receiveCancel()
	delivery, err := subscription.Receive(receiveCtx)
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if delivery.Event.Header.EventID != event.Header.EventID {
		t.Fatalf("event_id = %q, want %q", delivery.Event.Header.EventID, event.Header.EventID)
	}
	if delivery.Event.Header.Producer != event.Header.Producer {
		t.Fatalf("producer = %q, want %q", delivery.Event.Header.Producer, event.Header.Producer)
	}
	var payload map[string]string
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal delivered payload: %v", err)
	}
	if payload["record_id"] != "rec_test" || payload["value"] != "committed" {
		t.Fatalf("payload = %s, want record_id=rec_test value=committed", delivery.Event.Payload)
	}
	if err := subscription.Ack(ctx, delivery); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
}

func openCoreDB(t *testing.T) *sql.DB {
	t.Helper()
	config, err := base.LoadConfig(filepath.Join("..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}
	return db
}

func assertNoDelivery(t *testing.T, subscription eventbus.Subscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if delivery, err := subscription.Receive(ctx); err == nil {
		t.Fatalf("unexpected delivery before commit: %+v", delivery)
	}
}
