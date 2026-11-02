// base/records.go：excel_records 数据访问与单元格 JSON 编解码。
package base

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	commonv1 "excel/api/common/v1"
)

const (
	RecordsTable          = "excel_records"
	RecordsHistoriesTable = "excel_records_histories"
)

// ErrRecordNotFound identifies a missing or soft-deleted record.
var ErrRecordNotFound = errors.New("record not found")

// Record is the persistence projection of one table row.
type Record struct {
	ID        string
	TableID   string
	Cells     map[string]*commonv1.Value
	CreatedBy string
	CreatedAt int64
	Version   int64
}

// RecordHistory is one field-level append-only history row.
type RecordHistory struct {
	ID        string
	RecordID  string
	FieldID   string
	Before    *commonv1.Value
	After     *commonv1.Value
	Operator  string
	CreatedAt int64
	Seq       int64
}

// OutboxEvent is the durable sequence projection replayed to realtime.
type OutboxEvent struct {
	RowID        int64
	EffectiveSeq int64
	EventID      string
	Type         string
	Version      int32
	BaseID       string
	Seq          int64
	Payload      []byte
	OccurredAt   int64
	Producer     string
}

// RecordsRepository owns the records module's SQL.
type RecordsRepository struct {
	db *sql.DB
}

// NewRecordsRepository creates the records persistence gateway.
func NewRecordsRepository(db *sql.DB) *RecordsRepository {
	return &RecordsRepository{db: db}
}

// RecordsByTable loads active rows, optionally restricted to one creator.
func (repository *RecordsRepository) RecordsByTable(
	ctx context.Context,
	tableID, createdBy string,
) ([]Record, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("records database is required")
	}
	query := `
		SELECT id, table_id, cells_json, created_by, created_at, version
		FROM excel_records
		WHERE table_id = ? AND soft_deleted_at IS NULL`
	arguments := []any{tableID}
	if strings.TrimSpace(createdBy) != "" {
		query += " AND created_by = ?"
		arguments = append(arguments, createdBy)
	}
	query += " ORDER BY created_at, id"
	rows, err := repository.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query records by table: %w", err)
	}
	defer rows.Close()
	return scanRecords(rows)
}

// RecordByID loads one active row.
func (repository *RecordsRepository) RecordByID(
	ctx context.Context,
	recordID string,
) (Record, error) {
	if repository == nil || repository.db == nil {
		return Record{}, errors.New("records database is required")
	}
	record, err := scanRecord(repository.db.QueryRowContext(ctx, `
		SELECT id, table_id, cells_json, created_by, created_at, version
		FROM excel_records
		WHERE id = ? AND soft_deleted_at IS NULL
	`, recordID))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrRecordNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("query record: %w", err)
	}
	return record, nil
}

// RecordsByIDs loads the active subset of ids in tableID. Callers reapply the
// requested order after visibility filtering.
func (repository *RecordsRepository) RecordsByIDs(
	ctx context.Context,
	tableID string,
	recordIDs []string,
	createdBy string,
) ([]Record, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("records database is required")
	}
	if len(recordIDs) == 0 {
		return []Record{}, nil
	}
	placeholders := make([]string, len(recordIDs))
	arguments := make([]any, 0, len(recordIDs)+2)
	arguments = append(arguments, tableID)
	for index, recordID := range recordIDs {
		placeholders[index] = "?"
		arguments = append(arguments, recordID)
	}
	query := `
		SELECT id, table_id, cells_json, created_by, created_at, version
		FROM excel_records
		WHERE table_id = ?
		  AND soft_deleted_at IS NULL
		  AND id IN (` + strings.Join(placeholders, ", ") + `)`
	if strings.TrimSpace(createdBy) != "" {
		query += " AND created_by = ?"
		arguments = append(arguments, createdBy)
	}
	query += " ORDER BY created_at, id"
	rows, err := repository.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query records by ids: %w", err)
	}
	defer rows.Close()
	return scanRecords(rows)
}

// HistoriesByRecord returns append-only history newer than since, newest first.
func (repository *RecordsRepository) HistoriesByRecord(
	ctx context.Context,
	recordID string,
	since int64,
) ([]RecordHistory, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("records database is required")
	}
	rows, err := repository.db.QueryContext(ctx, `
		SELECT id, record_id, field_id, `+"`before`"+`, `+"`after`"+`, operator, created_at, seq
		FROM excel_records_histories
		WHERE record_id = ? AND created_at >= ?
		ORDER BY created_at DESC, seq DESC
	`, recordID, since)
	if err != nil {
		return nil, fmt.Errorf("query record histories: %w", err)
	}
	defer rows.Close()

	histories := make([]RecordHistory, 0)
	for rows.Next() {
		var history RecordHistory
		var before, after []byte
		if err := rows.Scan(
			&history.ID,
			&history.RecordID,
			&history.FieldID,
			&before,
			&after,
			&history.Operator,
			&history.CreatedAt,
			&history.Seq,
		); err != nil {
			return nil, fmt.Errorf("scan record history: %w", err)
		}
		if len(before) > 0 {
			value, err := DecodeRecordCell(before)
			if err != nil {
				return nil, fmt.Errorf("decode record history %s before: %w", history.ID, err)
			}
			history.Before = value
		}
		if len(after) > 0 {
			value, err := DecodeRecordCell(after)
			if err != nil {
				return nil, fmt.Errorf("decode record history %s after: %w", history.ID, err)
			}
			history.After = value
		}
		histories = append(histories, history)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate record histories: %w", err)
	}
	return histories, nil
}

// OutboxEventsSince reads events after lastSeq. Rows written before seq was
// populated use their positive auto-increment row id as the effective slot.
func (repository *RecordsRepository) OutboxEventsSince(
	ctx context.Context,
	baseID, tableID string,
	lastSeq int64,
	limit int,
) ([]OutboxEvent, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("records database is required")
	}
	query := `
		SELECT id, event_id, type, version, base_id, seq, payload, occurred_at, producer
		FROM excel_bus_outbox
		WHERE (seq > ? OR (seq = 0 AND id > ?))
		  AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.table_id')) = ?`
	arguments := []any{lastSeq, lastSeq, tableID}
	if strings.TrimSpace(baseID) != "" {
		query += " AND base_id = ?"
		arguments = append(arguments, baseID)
	}
	query += `
		ORDER BY CASE WHEN seq = 0 THEN id ELSE seq END, id
		LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := repository.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query outbox events since seq: %w", err)
	}
	defer rows.Close()

	events := make([]OutboxEvent, 0, limit)
	for rows.Next() {
		var event OutboxEvent
		var occurredAt time.Time
		if err := rows.Scan(
			&event.RowID,
			&event.EventID,
			&event.Type,
			&event.Version,
			&event.BaseID,
			&event.Seq,
			&event.Payload,
			&occurredAt,
			&event.Producer,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		event.EffectiveSeq = event.Seq
		if event.EffectiveSeq == 0 {
			event.EffectiveSeq = event.RowID
		}
		event.OccurredAt = occurredAt.UnixMilli()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox events: %w", err)
	}
	return events, nil
}

func scanRecords(rows *sql.Rows) ([]Record, error) {
	records := make([]Record, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("scan record: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate records: %w", err)
	}
	return records, nil
}

type recordsRowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(scanner recordsRowScanner) (Record, error) {
	var record Record
	var payload []byte
	if err := scanner.Scan(
		&record.ID,
		&record.TableID,
		&payload,
		&record.CreatedBy,
		&record.CreatedAt,
		&record.Version,
	); err != nil {
		return Record{}, err
	}
	cells, err := DecodeRecordCells(payload)
	if err != nil {
		return Record{}, err
	}
	record.Cells = cells
	return record, nil
}

// EncodeRecordCells stores each common.v1.Value as protobuf JSON under its
// field id. Keeping the JSON message form lets writers and readers share the
// same lossless cell representation.
func EncodeRecordCells(cells map[string]*commonv1.Value) ([]byte, error) {
	encoded := make(map[string]json.RawMessage, len(cells))
	marshaler := protojson.MarshalOptions{UseProtoNames: true}
	for fieldID, value := range cells {
		if strings.TrimSpace(fieldID) == "" {
			return nil, errors.New("record cell field_id is required")
		}
		if value == nil {
			return nil, fmt.Errorf("record cell %s value is required", fieldID)
		}
		payload, err := marshaler.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode record cell %s: %w", fieldID, err)
		}
		encoded[fieldID] = payload
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("encode record cells: %w", err)
	}
	return payload, nil
}

// EncodeRecordCell encodes one common.v1.Value for a cells_json or history
// JSON column.
func EncodeRecordCell(value *commonv1.Value) ([]byte, error) {
	if value == nil {
		return nil, errors.New("record cell value is required")
	}
	payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode record cell: %w", err)
	}
	return payload, nil
}

// DecodeRecordCell decodes one common.v1.Value stored as protobuf JSON.
func DecodeRecordCell(payload []byte) (*commonv1.Value, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	value := &commonv1.Value{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, value); err != nil {
		return nil, fmt.Errorf("decode record cell: %w", err)
	}
	return value, nil
}

// DecodeRecordCells decodes the cells_json representation.
func DecodeRecordCells(payload []byte) (map[string]*commonv1.Value, error) {
	cells := make(map[string]*commonv1.Value)
	if len(payload) == 0 {
		return cells, nil
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &encoded); err != nil {
		return nil, fmt.Errorf("decode record cells: %w", err)
	}
	unmarshaler := protojson.UnmarshalOptions{DiscardUnknown: true}
	for fieldID, raw := range encoded {
		value := &commonv1.Value{}
		if err := unmarshaler.Unmarshal(raw, value); err != nil {
			return nil, fmt.Errorf("decode record cell %s: %w", fieldID, err)
		}
		cells[fieldID] = value
	}
	return cells, nil
}
