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

	mysqlDriver "github.com/go-sql-driver/mysql"
	"google.golang.org/protobuf/encoding/protojson"

	commonv1 "excel/api/common/v1"
)

const (
	RecordsTable          = "excel_records"
	RecordsHistoriesTable = "excel_records_histories"
	RecordsOplogTable     = "excel_records_oplog"
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

// RecordState includes the soft-delete marker needed by mutation paths.
type RecordState struct {
	Record
	SoftDeletedAt *int64
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

// RecordOplogEntry is one idempotent write response.
type RecordOplogEntry struct {
	ClientRequestID string
	Kind            string
	RequestJSON     []byte
	ResponseJSON    []byte
	CreatedAt       int64
}

// RecordsRepository owns the records module's SQL.
type RecordsRepository struct {
	db *sql.DB
}

// NewRecordsRepository creates the records persistence gateway.
func NewRecordsRepository(db *sql.DB) *RecordsRepository {
	return &RecordsRepository{db: db}
}

// BeginTx starts one records mutation transaction.
func (repository *RecordsRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("records database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin records transaction: %w", err)
	}
	return tx, nil
}

// RecordByIDForUpdate locks one row, including soft-deleted rows, so the
// write path can distinguish missing rows from the 7-second undo window.
func (repository *RecordsRepository) RecordByIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	recordID string,
) (RecordState, error) {
	if tx == nil {
		return RecordState{}, errors.New("records transaction is required")
	}
	var state RecordState
	var payload []byte
	var softDeletedAt sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT id, table_id, cells_json, created_by, created_at, version, soft_deleted_at
		FROM excel_records
		WHERE id = ?
		FOR UPDATE
	`, recordID).Scan(
		&state.ID,
		&state.TableID,
		&payload,
		&state.CreatedBy,
		&state.CreatedAt,
		&state.Version,
		&softDeletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RecordState{}, ErrRecordNotFound
	}
	if err != nil {
		return RecordState{}, fmt.Errorf("lock record: %w", err)
	}
	cells, err := DecodeRecordCells(payload)
	if err != nil {
		return RecordState{}, err
	}
	state.Cells = cells
	if softDeletedAt.Valid {
		state.SoftDeletedAt = &softDeletedAt.Int64
	}
	return state, nil
}

// CreateRecordTx inserts one record row in the caller's transaction.
func (repository *RecordsRepository) CreateRecordTx(
	ctx context.Context,
	tx *sql.Tx,
	record Record,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	cellsJSON, err := EncodeRecordCells(record.Cells)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO excel_records (
			id, table_id, cells_json, created_by, created_at, version
		) VALUES (?, ?, ?, ?, ?, ?)
	`, record.ID, record.TableID, cellsJSON, record.CreatedBy,
		record.CreatedAt, record.Version)
	if err != nil {
		return fmt.Errorf("insert record: %w", err)
	}
	return nil
}

// CountActiveRecords locks and counts active rows for the table soft limit.
func (repository *RecordsRepository) CountActiveRecords(
	ctx context.Context,
	tx *sql.Tx,
	tableID string,
) (int64, error) {
	if tx == nil {
		return 0, errors.New("records transaction is required")
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM excel_records
		WHERE table_id = ? AND soft_deleted_at IS NULL
		FOR UPDATE
	`, tableID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active records: %w", err)
	}
	return count, nil
}

// UpdateRecordTx replaces cells and version for one already-locked row.
func (repository *RecordsRepository) UpdateRecordTx(
	ctx context.Context,
	tx *sql.Tx,
	record Record,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	cellsJSON, err := EncodeRecordCells(record.Cells)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_records
		SET cells_json = ?, version = ?
		WHERE id = ? AND soft_deleted_at IS NULL
	`, cellsJSON, record.Version, record.ID)
	if err != nil {
		return fmt.Errorf("update record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read record update result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("update record: affected rows = %d, want 1", affected)
	}
	return nil
}

// SoftDeleteRecordTx marks one locked record deleted and advances its version.
func (repository *RecordsRepository) SoftDeleteRecordTx(
	ctx context.Context,
	tx *sql.Tx,
	recordID string,
	version, deletedAt int64,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_records
		SET soft_deleted_at = ?, version = ?
		WHERE id = ? AND soft_deleted_at IS NULL
	`, deletedAt, version, recordID)
	if err != nil {
		return fmt.Errorf("soft delete record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read soft delete result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("soft delete record: affected rows = %d, want 1", affected)
	}
	return nil
}

// RestoreRecordTx clears one locked record's soft-delete marker.
func (repository *RecordsRepository) RestoreRecordTx(
	ctx context.Context,
	tx *sql.Tx,
	recordID string,
	version int64,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_records
		SET soft_deleted_at = NULL, version = ?
		WHERE id = ? AND soft_deleted_at IS NOT NULL
	`, version, recordID)
	if err != nil {
		return fmt.Errorf("restore record: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read restore result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("restore record: affected rows = %d, want 1", affected)
	}
	return nil
}

// AppendHistoryTx appends one immutable field-level history node.
func (repository *RecordsRepository) AppendHistoryTx(
	ctx context.Context,
	tx *sql.Tx,
	history RecordHistory,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	if history.ID == "" || history.RecordID == "" ||
		history.FieldID == "" || history.Operator == "" {
		return errors.New("record history identity, field and operator are required")
	}
	before, err := nullableEncodedCell(history.Before)
	if err != nil {
		return err
	}
	after, err := nullableEncodedCell(history.After)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO excel_records_histories (
			id, record_id, field_id, `+"`before`"+`, `+"`after`"+`, operator, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, history.ID, history.RecordID, history.FieldID, before, after,
		history.Operator, history.CreatedAt)
	if err != nil {
		return fmt.Errorf("append record history: %w", err)
	}
	return nil
}

func nullableEncodedCell(value *commonv1.Value) (any, error) {
	if value == nil {
		return nil, nil
	}
	return EncodeRecordCell(value)
}

// OplogByClientRequest finds one idempotency record regardless of age.
func (repository *RecordsRepository) OplogByClientRequest(
	ctx context.Context,
	clientRequestID string,
) (RecordOplogEntry, error) {
	if repository == nil || repository.db == nil {
		return RecordOplogEntry{}, errors.New("records database is required")
	}
	var entry RecordOplogEntry
	err := repository.db.QueryRowContext(ctx, `
		SELECT client_request_id, kind, request_json, response_json, created_at
		FROM excel_records_oplog
		WHERE client_request_id = ?
	`, clientRequestID).Scan(
		&entry.ClientRequestID,
		&entry.Kind,
		&entry.RequestJSON,
		&entry.ResponseJSON,
		&entry.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RecordOplogEntry{}, ErrRecordNotFound
	}
	if err != nil {
		return RecordOplogEntry{}, fmt.Errorf("query records oplog: %w", err)
	}
	return entry, nil
}

// DeleteExpiredOplogTx makes an expired client_request_id reusable.
func (repository *RecordsRepository) DeleteExpiredOplogTx(
	ctx context.Context,
	tx *sql.Tx,
	clientRequestID string,
	cutoff int64,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM excel_records_oplog
		WHERE client_request_id = ? AND created_at <= ?
	`, clientRequestID, cutoff); err != nil {
		return fmt.Errorf("delete expired records oplog: %w", err)
	}
	return nil
}

// InsertOplogTx reserves one idempotency key. A duplicate reports false.
func (repository *RecordsRepository) InsertOplogTx(
	ctx context.Context,
	tx *sql.Tx,
	entry RecordOplogEntry,
) (bool, error) {
	if tx == nil {
		return false, errors.New("records transaction is required")
	}
	if !json.Valid(entry.RequestJSON) || !json.Valid(entry.ResponseJSON) {
		return false, errors.New("records oplog JSON must be valid")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO excel_records_oplog (
			client_request_id, kind, request_json, response_json, created_at
		) VALUES (?, ?, ?, ?, ?)
	`, entry.ClientRequestID, entry.Kind, entry.RequestJSON,
		entry.ResponseJSON, entry.CreatedAt)
	if err == nil {
		return true, nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return false, nil
	}
	return false, fmt.Errorf("insert records oplog: %w", err)
}

// UpdateOplogResponseTx stores the terminal response before commit.
func (repository *RecordsRepository) UpdateOplogResponseTx(
	ctx context.Context,
	tx *sql.Tx,
	clientRequestID string,
	responseJSON []byte,
) error {
	if tx == nil {
		return errors.New("records transaction is required")
	}
	if !json.Valid(responseJSON) {
		return errors.New("records oplog response must be valid JSON")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_records_oplog
		SET response_json = ?
		WHERE client_request_id = ?
	`, responseJSON, clientRequestID)
	if err != nil {
		return fmt.Errorf("update records oplog response: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read records oplog response result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("records oplog response affected rows = %d, want 1", affected)
	}
	return nil
}

// Savepoint creates a per-operation rollback boundary in MySQL.
func (repository *RecordsRepository) Savepoint(
	ctx context.Context,
	tx *sql.Tx,
	name string,
) error {
	if tx == nil || name == "" {
		return errors.New("records transaction and savepoint name are required")
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+quoteIdentifier(name)); err != nil {
		return fmt.Errorf("create records savepoint: %w", err)
	}
	return nil
}

// RollbackToSavepoint restores one failed operation while preserving prior ops.
func (repository *RecordsRepository) RollbackToSavepoint(
	ctx context.Context,
	tx *sql.Tx,
	name string,
) error {
	if tx == nil || name == "" {
		return errors.New("records transaction and savepoint name are required")
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+quoteIdentifier(name)); err != nil {
		return fmt.Errorf("rollback records savepoint: %w", err)
	}
	return nil
}

// ReleaseSavepoint frees one per-operation savepoint after success or rollback.
func (repository *RecordsRepository) ReleaseSavepoint(
	ctx context.Context,
	tx *sql.Tx,
	name string,
) error {
	if tx == nil || name == "" {
		return errors.New("records transaction and savepoint name are required")
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+quoteIdentifier(name)); err != nil {
		return fmt.Errorf("release records savepoint: %w", err)
	}
	return nil
}

func quoteIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
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

// HistoryByID loads one immutable node for rollback.
func (repository *RecordsRepository) HistoryByID(
	ctx context.Context,
	historyID string,
) (RecordHistory, error) {
	if repository == nil || repository.db == nil {
		return RecordHistory{}, errors.New("records database is required")
	}
	var history RecordHistory
	var before, after []byte
	err := repository.db.QueryRowContext(ctx, `
		SELECT id, record_id, field_id, `+"`before`"+`, `+"`after`"+`, operator, created_at, seq
		FROM excel_records_histories
		WHERE id = ?
	`, historyID).Scan(
		&history.ID,
		&history.RecordID,
		&history.FieldID,
		&before,
		&after,
		&history.Operator,
		&history.CreatedAt,
		&history.Seq,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RecordHistory{}, ErrRecordNotFound
	}
	if err != nil {
		return RecordHistory{}, fmt.Errorf("query record history: %w", err)
	}
	if len(before) > 0 {
		history.Before, err = DecodeRecordCell(before)
		if err != nil {
			return RecordHistory{}, err
		}
	}
	if len(after) > 0 {
		history.After, err = DecodeRecordCell(after)
		if err != nil {
			return RecordHistory{}, err
		}
	}
	return history, nil
}

// OutboxEventsSince reads events after lastSeq. Rows written before seq was
// populated use their positive auto-increment row id as the effective slot.
// 跨模块 SQL（§2.5）：断线补发需按 table 过滤 outbox 事件流并与记录 seq 对账；outbox 与记录同库，直接过滤避免拉全量后丢弃。
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
