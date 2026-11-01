package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	// BusConsumerTable stores worker-side event_id deduplication state.
	BusConsumerTable = "excel_bus_consumer_events"
)

// ConsumerStore owns the worker-side event_id deduplication table.
type ConsumerStore struct {
	db *sql.DB
}

// NewConsumerStore creates the worker EventBus persistence gateway.
func NewConsumerStore(db *sql.DB) *ConsumerStore {
	return &ConsumerStore{db: db}
}

// Reserve claims an event_id for exactly-once business handling. A previously
// processed or dead event is ignored; a failed event may be retried.
func (s *ConsumerStore) Reserve(ctx context.Context, eventID string, consumer string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO excel_bus_consumer_events (
			event_id, consumer_name, status, attempts, created_at, updated_at
		) VALUES (?, ?, 'processing', 0, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`, eventID, consumer)
	if err != nil {
		return false, fmt.Errorf("reserve consumed event: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reserve consumed event: read affected rows: %w", err)
	}
	if inserted == 1 {
		return true, nil
	}

	var status string
	if err := s.db.QueryRowContext(ctx, `
		SELECT status
		FROM excel_bus_consumer_events
		WHERE event_id = ?
	`, eventID).Scan(&status); err != nil {
		return false, fmt.Errorf("read consumed event state: %w", err)
	}
	if status != "failed" {
		return false, nil
	}
	result, err = s.db.ExecContext(ctx, `
		UPDATE excel_bus_consumer_events
		SET status = 'processing', updated_at = UTC_TIMESTAMP(3)
		WHERE event_id = ? AND status = 'failed'
	`, eventID)
	if err != nil {
		return false, fmt.Errorf("retry failed consumed event: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("retry failed consumed event: read affected rows: %w", err)
	}
	return updated == 1, nil
}

// MarkProcessed permanently completes an event_id.
func (s *ConsumerStore) MarkProcessed(ctx context.Context, eventID string, attempts int) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE excel_bus_consumer_events
		SET status = 'processed',
		    attempts = ?,
		    last_error = NULL,
		    processed_at = UTC_TIMESTAMP(3),
		    updated_at = UTC_TIMESTAMP(3)
		WHERE event_id = ? AND status = 'processing'
	`, attempts, eventID)
	if err != nil {
		return fmt.Errorf("mark consumed event processed: %w", err)
	}
	return requireOneConsumerRow(result, "mark consumed event processed")
}

// MarkFailed records a retryable consumer failure.
func (s *ConsumerStore) MarkFailed(ctx context.Context, eventID string, attempts int, consumeErr error) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE excel_bus_consumer_events
		SET status = 'failed',
		    attempts = ?,
		    last_error = ?,
		    updated_at = UTC_TIMESTAMP(3)
		WHERE event_id = ? AND status = 'processing'
	`, attempts, truncateConsumerError(consumeErr), eventID)
	if err != nil {
		return fmt.Errorf("mark consumed event failed: %w", err)
	}
	return requireOneConsumerRow(result, "mark consumed event failed")
}

// MarkDead records an event whose retry budget was exhausted.
func (s *ConsumerStore) MarkDead(ctx context.Context, eventID string, attempts int, consumeErr error) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE excel_bus_consumer_events
		SET status = 'dead',
		    attempts = ?,
		    last_error = ?,
		    updated_at = UTC_TIMESTAMP(3)
		WHERE event_id = ? AND status = 'processing'
	`, attempts, truncateConsumerError(consumeErr), eventID)
	if err != nil {
		return fmt.Errorf("mark consumed event dead: %w", err)
	}
	return requireOneConsumerRow(result, "mark consumed event dead")
}

func requireOneConsumerRow(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: read affected rows: %w", operation, err)
	}
	if affected != 1 {
		return errors.New(operation + ": event state did not change")
	}
	return nil
}

func truncateConsumerError(err error) string {
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
