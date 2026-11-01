// base/audit.go：对应 excel_audit_events 表的数据操作（A 模块仅操作 A 模块表）。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

const AuditEventsTable = "excel_audit_events"

// ErrAuditEventExists reports that an event was already appended.
var ErrAuditEventExists = errors.New("audit event already exists")

// AuditEventRecord is one append-only, action-level audit row.
type AuditEventRecord struct {
	ID           int64
	EventID      string
	EnterpriseID string
	Action       string
	Operator     string
	Target       string
	BaseID       string
	TableID      string
	Detail       []byte
	OccurredAt   time.Time
	CreatedAt    time.Time
}

// AuditQuery selects audit rows from one enterprise and optional time window.
type AuditQuery struct {
	EnterpriseID string
	Operator     string
	From         time.Time
	To           time.Time
	Cursor       int64
	Limit        int
}

// AuditRepository owns the INSERT and SELECT operations for audit events.
type AuditRepository struct {
	db *sql.DB
}

// NewAuditRepository creates the audit persistence gateway.
func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Append inserts one event. The unique event_id makes retries observable as
// ErrAuditEventExists while never rewriting an existing audit row.
func (repository *AuditRepository) Append(
	ctx context.Context,
	record AuditEventRecord,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("audit database is required")
	}
	var detail any
	if len(record.Detail) > 0 {
		detail = record.Detail
	}
	_, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_audit_events (
			event_id, enterprise_id, action, operator, target,
			base_id, table_id, detail, occurred_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(3))
	`,
		record.EventID,
		record.EnterpriseID,
		record.Action,
		record.Operator,
		record.Target,
		nullableAuditString(record.BaseID),
		nullableAuditString(record.TableID),
		detail,
		record.OccurredAt.UTC(),
	)
	if err == nil {
		return nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrAuditEventExists
	}
	return fmt.Errorf("insert audit event: %w", err)
}

// List returns audit rows newest first using id as the stable cursor.
func (repository *AuditRepository) List(
	ctx context.Context,
	query AuditQuery,
) ([]AuditEventRecord, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("audit database is required")
	}
	if query.EnterpriseID == "" {
		return nil, errors.New("audit enterprise_id is required")
	}
	if query.Limit <= 0 || query.Limit > 1000 {
		query.Limit = 20
	}

	var conditions = []string{"enterprise_id = ?"}
	var arguments = []any{query.EnterpriseID}
	if query.Operator != "" {
		conditions = append(conditions, "operator = ?")
		arguments = append(arguments, query.Operator)
	}
	if !query.From.IsZero() {
		conditions = append(conditions, "occurred_at >= ?")
		arguments = append(arguments, query.From.UTC())
	}
	if !query.To.IsZero() {
		conditions = append(conditions, "occurred_at <= ?")
		arguments = append(arguments, query.To.UTC())
	}
	if query.Cursor > 0 {
		conditions = append(conditions, "id < ?")
		arguments = append(arguments, query.Cursor)
	}
	arguments = append(arguments, query.Limit)

	rows, err := repository.db.QueryContext(ctx, `
		SELECT id, event_id, enterprise_id, action, operator, target,
		       base_id, table_id, detail, occurred_at, created_at
		FROM excel_audit_events
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY id DESC
		LIMIT ?
	`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	records := make([]AuditEventRecord, 0, query.Limit)
	for rows.Next() {
		var record AuditEventRecord
		var baseID, tableID sql.NullString
		var detail []byte
		if err := rows.Scan(
			&record.ID,
			&record.EventID,
			&record.EnterpriseID,
			&record.Action,
			&record.Operator,
			&record.Target,
			&baseID,
			&tableID,
			&detail,
			&record.OccurredAt,
			&record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		record.BaseID = baseID.String
		record.TableID = tableID.String
		record.Detail = detail
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return records, nil
}

func nullableAuditString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
