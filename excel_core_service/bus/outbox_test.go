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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

// TestClaimUnpublishedFillsZeroSeqWithOutboxRowID 锁 issue 01：事件头 seq=0 时，
// 转发发布的序号取 outbox 行持久自增 id，且与 OutboxEventsSince 的有效序号一致；
// 已有非零 seq 保持不变。claim 使用远未来时间并推迟 next_publish_at，
// 避免与共享库上的并发 forwarder 抢同一批行。
func TestClaimUnpublishedFillsZeroSeqWithOutboxRowID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := openCoreDB(t)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	topic := fmt.Sprintf("test.outbox.seq.%d", time.Now().UnixNano())

	suffix := time.Now().UnixNano()
	baseID := fmt.Sprintf("base_seq_%d", suffix)
	tableID := fmt.Sprintf("tbl_seq_%d", suffix)
	zeroEventID := fmt.Sprintf("evt_zero_%d", suffix)
	explicitEventID := fmt.Sprintf("evt_explicit_%d", suffix)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM excel_bus_outbox WHERE event_id IN (?, ?)`, zeroEventID, explicitEventID)
	})

	enqueue := func(eventID string, seq int64) {
		t.Helper()
		event := eventbus.Event{
			Header: eventbus.EventHeader{
				EventID:    eventID,
				Type:       topic,
				Version:    1,
				BaseID:     baseID,
				Seq:        seq,
				OccurredAt: time.Now().UnixMilli(),
				Producer:   "excel_core_service.records",
			},
			Payload: json.RawMessage(fmt.Sprintf(`{"table_id":%q,"record_id":"rec_seq"}`, tableID)),
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx() error = %v", err)
		}
		if err := outbox.Enqueue(ctx, tx, event); err != nil {
			_ = tx.Rollback()
			t.Fatalf("Enqueue(%s) error = %v", eventID, err)
		}
		// 同一事务内把 next_publish_at 推到未来，避免共享库上的并发 forwarder
		// 在提交后、断言前先认领并发布这两行。
		if _, err := tx.ExecContext(ctx,
			`UPDATE excel_bus_outbox SET next_publish_at = ? WHERE event_id = ?`,
			time.Now().UTC().Add(time.Hour), eventID,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("defer next_publish_at(%s) error = %v", eventID, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit(%s) error = %v", eventID, err)
		}
	}
	enqueue(zeroEventID, 0)
	enqueue(explicitEventID, 77)

	claimTx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() for claim error = %v", err)
	}
	defer func() { _ = claimTx.Rollback() }()
	items, err := store.ClaimUnpublished(ctx, claimTx, 1000, time.Now().UTC().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ClaimUnpublished() error = %v", err)
	}
	claimed := make(map[string]int64, 2)
	for _, item := range items {
		if item.Event.Header.EventID == zeroEventID ||
			item.Event.Header.EventID == explicitEventID {
			claimed[item.Event.Header.EventID] = item.Event.Header.Seq
		}
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed events = %v, want both test events", claimed)
	}

	var zeroRowID int64
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM excel_bus_outbox WHERE event_id = ?`, zeroEventID,
	).Scan(&zeroRowID); err != nil {
		t.Fatalf("query outbox row id error = %v", err)
	}
	if zeroRowID <= 0 {
		t.Fatalf("outbox row id = %d, want positive", zeroRowID)
	}
	if got := claimed[zeroEventID]; got != zeroRowID {
		t.Fatalf("claimed zero-seq event seq = %d, want outbox row id %d", got, zeroRowID)
	}
	if got := claimed[explicitEventID]; got != 77 {
		t.Fatalf("claimed explicit-seq event seq = %d, want 77", got)
	}

	repository := base.NewRecordsRepository(db)
	events, err := repository.OutboxEventsSince(ctx, baseID, tableID, 0, 10)
	if err != nil {
		t.Fatalf("OutboxEventsSince() error = %v", err)
	}
	effective := make(map[string]int64, len(events))
	for _, event := range events {
		effective[event.EventID] = event.EffectiveSeq
	}
	for _, eventID := range []string{zeroEventID, explicitEventID} {
		if effective[eventID] != claimed[eventID] {
			t.Fatalf("effective seq for %s = %d, claimed seq = %d; want identical",
				eventID, effective[eventID], claimed[eventID])
		}
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
