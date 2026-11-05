package tracking_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"excel-libs/eventbus"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/tracking"
)

type trackingHarness struct {
	db        *sql.DB
	broker    *eventbus.MemoryBus
	forwarder *bus.Forwarder
	outbox    *bus.Outbox
	consumer  *bus.Consumer
}

func newTrackingHarness(t *testing.T) *trackingHarness {
	t.Helper()

	config, err := base.LoadConfig(filepath.Join("..", "..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("base.LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	if err := base.MigrateUp(db); err != nil {
		_ = db.Close()
		t.Fatalf("base.MigrateUp() error = %v", err)
	}

	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	broker := eventbus.NewMemoryBus()
	service := tracking.NewService(base.NewTrackingRepository(db))
	consumer := bus.NewConsumer(store, broker, bus.ConsumerConfig{
		Retry: eventbus.RetryPolicy{
			Initial:     10 * time.Millisecond,
			Max:         20 * time.Millisecond,
			MaxAttempts: 3,
		},
	})
	if err := tracking.Consumers(consumer, service); err != nil {
		_ = db.Close()
		t.Fatalf("tracking.Consumers() error = %v", err)
	}
	consumerContext, cancelConsumer := context.WithCancel(context.Background())
	if err := consumer.Start(consumerContext); err != nil {
		cancelConsumer()
		_ = db.Close()
		t.Fatalf("consumer.Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = consumer.Stop(stopContext)
		cancelConsumer()
		cleanupTracking(db)
		_ = db.Close()
	})
	return &trackingHarness{
		db:        db,
		broker:    broker,
		forwarder: bus.NewForwarder(store, broker, bus.ForwarderConfig{BatchSize: 100}),
		outbox:    outbox,
		consumer:  consumer,
	}
}

type trackingEventFixture struct {
	envelopeID string
	payload    map[string]any
}

func enqueueTrackingEvents(
	t *testing.T,
	harness *trackingHarness,
	fixtures []trackingEventFixture,
) {
	t.Helper()
	tx, err := harness.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for index, fixture := range fixtures {
		payload, err := json.Marshal(fixture.payload)
		if err != nil {
			t.Fatalf("marshal track.reported payload: %v", err)
		}
		event := eventbus.Event{
			Header: eventbus.EventHeader{
				EventID:    fixture.envelopeID,
				Type:       "track.reported",
				Version:    1,
				OccurredAt: time.Now().Add(time.Duration(index) * time.Millisecond).UnixMilli(),
				Producer:   "tracking-test",
			},
			Payload: payload,
		}
		if err := harness.outbox.Enqueue(context.Background(), tx, event); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", fixture.envelopeID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit synthetic tracking events: %v", err)
	}
}

func cleanupTracking(db *sql.DB) {
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DELETE FROM excel_tracking_events WHERE event_id LIKE '01J00000000000000000009%'")
	_, _ = db.ExecContext(ctx, "DELETE FROM excel_bus_outbox WHERE event_id LIKE '01J00000000000000000009%'")
}

type trackingRow struct {
	EventID      string
	UserID       string
	UserType     string
	EventName    string
	Detail       []byte
	ClientAtMs   sql.NullInt64
	ReceivedAtMs int64
}

func loadTrackingRows(t *testing.T, db *sql.DB) map[string]trackingRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT event_id, user_id, user_type, event_name, detail, client_at_ms, received_at_ms
		FROM excel_tracking_events
		WHERE event_id LIKE '01J00000000000000000009%'
	`)
	if err != nil {
		t.Fatalf("query tracking rows: %v", err)
	}
	defer rows.Close()
	result := make(map[string]trackingRow)
	for rows.Next() {
		var row trackingRow
		if err := rows.Scan(
			&row.EventID,
			&row.UserID,
			&row.UserType,
			&row.EventName,
			&row.Detail,
			&row.ClientAtMs,
			&row.ReceivedAtMs,
		); err != nil {
			t.Fatalf("scan tracking row: %v", err)
		}
		result[row.EventID] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tracking rows: %v", err)
	}
	return result
}

func outboxStatus(t *testing.T, db *sql.DB, eventID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(context.Background(), `
		SELECT status FROM excel_bus_outbox WHERE event_id = ?
	`, eventID).Scan(&status); err != nil {
		t.Fatalf("query outbox status for %s: %v", eventID, err)
	}
	return status
}

func TestTrackingEventsAreSunkIdempotentlyAndAcked(t *testing.T) {
	harness := newTrackingHarness(t)
	const (
		userID     = "01J00000000000000000009u1"
		internalID = "01J00000000000000000009u2"
		eventA     = "01J00000000000000000009e1"
		eventB     = "01J00000000000000000009e2"
		envelopeA1 = "01J00000000000000000009n1"
		envelopeA2 = "01J00000000000000000009n2" // 同一埋点 event_id 的重投递
		envelopeB  = "01J00000000000000000009n3"
	)

	fixtures := []trackingEventFixture{
		{
			envelopeID: envelopeA1,
			payload: map[string]any{
				"event_id":       eventA,
				"event_name":     "fe_record_opened",
				"detail":         map[string]any{"base_id": "01J00000000000000000009b1"},
				"client_at_ms":   1000,
				"user_id":        userID,
				"user_type":      "external",
				"received_at_ms": 2000,
			},
		},
		{
			envelopeID: envelopeA2,
			payload: map[string]any{
				"event_id":       eventA,
				"event_name":     "fe_record_opened",
				"detail":         map[string]any{"base_id": "01J00000000000000000009b1"},
				"client_at_ms":   1000,
				"user_id":        userID,
				"user_type":      "external",
				"received_at_ms": 2001,
			},
		},
		{
			envelopeID: envelopeB,
			payload: map[string]any{
				"event_id":       eventB,
				"event_name":     "fe_comment_created",
				"detail":         map[string]any{"record_id": "01J00000000000000000009r1"},
				"user_id":        internalID,
				"user_type":      "internal",
				"received_at_ms": 3000,
			},
		},
	}
	enqueueTrackingEvents(t, harness, fixtures)
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var rows map[string]trackingRow
	for time.Now().Before(deadline) {
		rows = loadTrackingRows(t, harness.db)
		if len(rows) == 2 && outboxStatus(t, harness.db, envelopeB) == "acked" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(rows) != 2 {
		t.Fatalf("tracking rows = %d, want 2 (duplicate event_id must not add a row); rows = %+v", len(rows), rows)
	}

	opened := rows[eventA]
	if opened.UserID != userID ||
		opened.UserType != "external" ||
		opened.EventName != "fe_record_opened" ||
		opened.ReceivedAtMs != 2000 ||
		!opened.ClientAtMs.Valid ||
		opened.ClientAtMs.Int64 != 1000 {
		t.Fatalf("record_opened row = %+v", opened)
	}
	var openedDetail map[string]any
	if err := json.Unmarshal(opened.Detail, &openedDetail); err != nil ||
		openedDetail["base_id"] != "01J00000000000000000009b1" {
		t.Fatalf("record_opened detail = %s, %v", opened.Detail, err)
	}

	commented := rows[eventB]
	if commented.UserID != internalID ||
		commented.UserType != "internal" ||
		commented.EventName != "fe_comment_created" ||
		commented.ReceivedAtMs != 3000 ||
		commented.ClientAtMs.Valid {
		t.Fatalf("comment_created row = %+v (client_at_ms omitted should be NULL)", commented)
	}

	for _, envelopeID := range []string{envelopeA1, envelopeA2, envelopeB} {
		if status := outboxStatus(t, harness.db, envelopeID); status != "acked" {
			t.Fatalf("outbox %s status = %q, want acked", envelopeID, status)
		}
	}
}