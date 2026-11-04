// base/tracking.go：对应 excel_tracking_events 表的数据操作（A 模块仅操作 A 模块表）。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// TrackingEventsTable is the client-side tracking event sink, kept separate from
// the append-only excel_audit_events so tracking volume cannot pollute audit
// semantics (audit 不可删，埋点可保留期清理）。
const TrackingEventsTable = "excel_tracking_events"

// ErrTrackingEventExists reports that a tracking event was already stored.
var ErrTrackingEventExists = errors.New("tracking event already exists")

// TrackingEventRecord is one persisted client tracking event.
type TrackingEventRecord struct {
	EventID      string
	UserID       string
	UserType     string
	EventName    string
	Detail       []byte
	ClientAtMs   int64
	ReceivedAtMs int64
}

// TrackingRepository owns the INSERT used by the tracking sink.
type TrackingRepository struct {
	db *sql.DB
}

// NewTrackingRepository creates the tracking persistence gateway.
func NewTrackingRepository(db *sql.DB) *TrackingRepository {
	return &TrackingRepository{db: db}
}

// Insert stores one event. The unique event_id makes redelivery observable as
// ErrTrackingEventExists while never adding a duplicate row.
func (repository *TrackingRepository) Insert(
	ctx context.Context,
	record TrackingEventRecord,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("tracking database is required")
	}
	var detail any
	if len(record.Detail) > 0 {
		detail = record.Detail
	}
	_, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_tracking_events (
			event_id, user_id, user_type, event_name, detail,
			client_at_ms, received_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		record.EventID,
		record.UserID,
		record.UserType,
		record.EventName,
		detail,
		nullableTrackingMillis(record.ClientAtMs),
		record.ReceivedAtMs,
	)
	if err == nil {
		return nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrTrackingEventExists
	}
	return fmt.Errorf("insert tracking event: %w", err)
}

func nullableTrackingMillis(millis int64) any {
	if millis == 0 {
		return nil
	}
	return millis
}