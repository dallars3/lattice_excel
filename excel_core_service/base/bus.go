package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"excel-libs/eventbus"
)

const (
	// BusOutboxTable stores events written in the same transaction as business data.
	BusOutboxTable = "excel_bus_outbox"
)

// BusStore owns all SQL for the outbox and dead-letter tables.
type BusStore struct {
	db *sql.DB
}

// NewBusStore creates the core EventBus persistence gateway.
func NewBusStore(db *sql.DB) *BusStore {
	return &BusStore{db: db}
}

// BeginTx starts a transaction for outbox forwarding.
func (s *BusStore) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("bus database is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bus transaction: %w", err)
	}
	return tx, nil
}

// OutboxItem is an outbox row claimed by the forwarder.
type OutboxItem struct {
	Event           eventbus.Event
	Status          string
	PublishAttempts int
	ConsumeAttempts int
	NextConsumeAt   sql.NullTime
}

// ConsumeState contains the durable retry state for one consumed event.
type ConsumeState struct {
	Status        string
	Attempts      int
	NextConsumeAt sql.NullTime
	LastError     sql.NullString
	EventExists   bool
}

// DeadLetter is an event that exhausted consumer retry attempts.
type DeadLetter struct {
	EventID    string
	Type       string
	Payload    []byte
	Attempts   int
	LastError  string
	OccurredAt time.Time
	DeadAt     time.Time
}

// Enqueue writes an event into the outbox using the caller's business transaction.
func (s *BusStore) Enqueue(ctx context.Context, tx *sql.Tx, event eventbus.Event) error {
	if tx == nil {
		return errors.New("business transaction is required")
	}
	if err := event.Validate(); err != nil {
		return fmt.Errorf("validate outbox event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO excel_bus_outbox (
			event_id, type, version, base_id, seq, payload, status, occurred_at, producer,
			publish_attempts, consume_attempts, next_publish_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'sent', ?, ?, 0, 0, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`,
		event.Header.EventID,
		event.Header.Type,
		event.Header.Version,
		event.Header.BaseID,
		event.Header.Seq,
		[]byte(event.Payload),
		time.UnixMilli(event.Header.OccurredAt).UTC(),
		event.Header.Producer,
	); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// ClaimUnpublished locks a batch of publishable outbox events with SKIP LOCKED.
func (s *BusStore) ClaimUnpublished(ctx context.Context, tx *sql.Tx, limit int, now time.Time) ([]OutboxItem, error) {
	if limit <= 0 {
		return nil, errors.New("claim limit must be positive")
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT event_id, type, version, base_id, seq, payload, occurred_at, producer,
		       publish_attempts
		FROM excel_bus_outbox
		WHERE status = 'sent'
		  AND published_at IS NULL
		  AND next_publish_at <= ?
		ORDER BY id
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("claim unpublished outbox events: %w", err)
	}
	defer rows.Close()

	items := make([]OutboxItem, 0, limit)
	for rows.Next() {
		var item OutboxItem
		var occurredAt time.Time
		if err := rows.Scan(
			&item.Event.Header.EventID,
			&item.Event.Header.Type,
			&item.Event.Header.Version,
			&item.Event.Header.BaseID,
			&item.Event.Header.Seq,
			&item.Event.Payload,
			&occurredAt,
			&item.Event.Header.Producer,
			&item.PublishAttempts,
		); err != nil {
			return nil, fmt.Errorf("scan unpublished outbox event: %w", err)
		}
		item.Event.Header.OccurredAt = occurredAt.UnixMilli()
		item.Status = "sent"
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unpublished outbox events: %w", err)
	}
	return items, nil
}

// MarkPublished records a successful transport publish while awaiting consumer ack.
func (s *BusStore) MarkPublished(ctx context.Context, tx *sql.Tx, eventID string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_bus_outbox
		SET publish_attempts = publish_attempts + 1,
		    published_at = ?,
		    next_publish_at = NULL,
		    last_error = NULL,
		    updated_at = ?
		WHERE event_id = ? AND status = 'sent'
	`, now.UTC(), now.UTC(), eventID)
	if err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	return requireOneRow(result, "mark outbox event published")
}

// MarkPublishFailure schedules a publish retry or moves the event to dead.
func (s *BusStore) MarkPublishFailure(
	ctx context.Context,
	tx *sql.Tx,
	item OutboxItem,
	publishErr error,
	nextAttemptAt time.Time,
	dead bool,
	now time.Time,
) error {
	attempts := item.PublishAttempts + 1
	if dead {
		result, err := tx.ExecContext(ctx, `
			UPDATE excel_bus_outbox
			SET status = 'dead',
			    publish_attempts = ?,
			    next_publish_at = NULL,
			    dead_at = ?,
			    last_error = ?,
			    updated_at = ?
			WHERE event_id = ? AND status = 'sent'
		`, attempts, now.UTC(), truncateError(publishErr), now.UTC(), item.Event.Header.EventID)
		if err != nil {
			return fmt.Errorf("mark outbox publish dead: %w", err)
		}
		return requireOneRow(result, "mark outbox publish dead")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE excel_bus_outbox
		SET publish_attempts = ?,
		    next_publish_at = ?,
		    last_error = ?,
		    updated_at = ?
		WHERE event_id = ? AND status = 'sent'
	`, attempts, nextAttemptAt.UTC(), truncateError(publishErr), now.UTC(), item.Event.Header.EventID)
	if err != nil {
		return fmt.Errorf("schedule outbox publish retry: %w", err)
	}
	return requireOneRow(result, "schedule outbox publish retry")
}

// Ack marks an outbox event fully consumed.
func (s *BusStore) Ack(ctx context.Context, eventID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE excel_bus_outbox
		SET status = 'acked',
		    acked_at = ?,
		    updated_at = ?
		WHERE event_id = ? AND status = 'sent'
	`, now.UTC(), now.UTC(), eventID)
	if err != nil {
		return fmt.Errorf("ack outbox event: %w", err)
	}
	return requireOneRow(result, "ack outbox event")
}

// ConsumeState returns the durable retry state used before invoking a handler.
func (s *BusStore) ConsumeState(ctx context.Context, eventID string) (ConsumeState, error) {
	var state ConsumeState
	err := s.db.QueryRowContext(ctx, `
		SELECT status, consume_attempts, next_consume_at, last_error
		FROM excel_bus_outbox
		WHERE event_id = ?
	`, eventID).Scan(&state.Status, &state.Attempts, &state.NextConsumeAt, &state.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return ConsumeState{}, fmt.Errorf("read outbox consume state: %w", err)
	}
	state.EventExists = true
	return state, nil
}

// MarkConsumeFailure records one failed handler attempt and creates a dead
// letter when the configured retry budget is exhausted.
func (s *BusStore) MarkConsumeFailure(
	ctx context.Context,
	item OutboxItem,
	consumeErr error,
	nextAttemptAt time.Time,
	dead bool,
	now time.Time,
) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin consume failure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var attempts int
	if err := tx.QueryRowContext(ctx, `
		SELECT consume_attempts
		FROM excel_bus_outbox
		WHERE event_id = ?
		FOR UPDATE
	`, item.Event.Header.EventID).Scan(&attempts); err != nil {
		return 0, fmt.Errorf("lock outbox consume state: %w", err)
	}
	attempts++

	if dead {
		result, err := tx.ExecContext(ctx, `
			UPDATE excel_bus_outbox
			SET status = 'dead',
			    consume_attempts = ?,
			    next_consume_at = NULL,
			    dead_at = ?,
			    last_error = ?,
			    updated_at = ?
			WHERE event_id = ? AND status = 'sent'
		`, attempts, now.UTC(), truncateError(consumeErr), now.UTC(), item.Event.Header.EventID)
		if err != nil {
			return 0, fmt.Errorf("mark outbox consume dead: %w", err)
		}
		if err := requireOneRow(result, "mark outbox consume dead"); err != nil {
			return 0, err
		}
	} else {
		result, err := tx.ExecContext(ctx, `
			UPDATE excel_bus_outbox
			SET consume_attempts = ?,
			    next_consume_at = ?,
			    last_error = ?,
			    updated_at = ?
			WHERE event_id = ? AND status = 'sent'
		`, attempts, nextAttemptAt.UTC(), truncateError(consumeErr), now.UTC(), item.Event.Header.EventID)
		if err != nil {
			return 0, fmt.Errorf("schedule outbox consume retry: %w", err)
		}
		if err := requireOneRow(result, "schedule outbox consume retry"); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit outbox consume failure: %w", err)
	}
	return attempts, nil
}

// DeadLetters lists dead events for operational and test queries.
func (s *BusStore) DeadLetters(ctx context.Context, limit int) ([]DeadLetter, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, type, payload,
		       GREATEST(publish_attempts, consume_attempts),
		       last_error, occurred_at, dead_at
		FROM excel_bus_outbox
		WHERE status = 'dead'
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query dead letters: %w", err)
	}
	defer rows.Close()

	letters := make([]DeadLetter, 0, limit)
	for rows.Next() {
		var letter DeadLetter
		if err := rows.Scan(
			&letter.EventID,
			&letter.Type,
			&letter.Payload,
			&letter.Attempts,
			&letter.LastError,
			&letter.OccurredAt,
			&letter.DeadAt,
		); err != nil {
			return nil, fmt.Errorf("scan dead letter: %w", err)
		}
		letters = append(letters, letter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dead letters: %w", err)
	}
	return letters, nil
}

func requireOneRow(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: read affected rows: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf("%s: affected rows = %d, want 1", operation, affected)
	}
	return nil
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	const maxErrorLength = 1024
	message := err.Error()
	if len(message) <= maxErrorLength {
		return message
	}
	return message[:maxErrorLength]
}
