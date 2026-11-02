// base/calc.go：对应 excel_calc 表的数据操作（A 模块仅操作 A 模块表，跨模块在逻辑层拆分/聚合）。
package base

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

const (
	CalcJobsTable         = "excel_calc_jobs"
	CalcMaterializedTable = "excel_calc_materialized"
)

// ErrCalcJobExists reports a duplicate calc job. The unique job_id is the
// idempotency boundary for calc.job delivery.
var ErrCalcJobExists = errors.New("calc job already exists")

// CalcRepository owns the calc job and materialization tables.
type CalcRepository struct {
	db *sql.DB
}

// NewCalcRepository creates the calc persistence gateway.
func NewCalcRepository(db *sql.DB) *CalcRepository {
	return &CalcRepository{db: db}
}

// BeginTx starts a transaction used to atomically persist materializations and
// enqueue record.changed events.
func (repository *CalcRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("calc database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin calc transaction: %w", err)
	}
	return tx, nil
}

// CreateJob claims a job_id for one execution. It returns false for a duplicate.
func (repository *CalcRepository) CreateJob(
	ctx context.Context,
	jobID, tableID string,
	payload []byte,
) (bool, error) {
	if repository == nil || repository.db == nil {
		return false, errors.New("calc database is required")
	}
	if jobID == "" || tableID == "" {
		return false, errors.New("calc job_id and table_id are required")
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return false, errors.New("calc payload must be valid JSON")
	}
	_, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_calc_jobs (
			job_id, table_id, status, payload, attempts, created_at, updated_at
		) VALUES (?, ?, 'CALC', ?, 1, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`, jobID, tableID, payload)
	if err == nil {
		return true, nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return false, nil
	}
	return false, fmt.Errorf("insert calc job: %w", err)
}

// FinishJob commits the terminal job status.
func (repository *CalcRepository) FinishJob(
	ctx context.Context,
	tx *sql.Tx,
	jobID, status string,
) error {
	if tx == nil {
		return errors.New("calc transaction is required")
	}
	if !validCalcJobStatus(status) {
		return fmt.Errorf("invalid calc job status %q", status)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_calc_jobs
		SET status = ?, updated_at = UTC_TIMESTAMP(3)
		WHERE job_id = ?
	`, status, jobID)
	if err != nil {
		return fmt.Errorf("finish calc job: %w", err)
	}
	return requireOneRow(result, "finish calc job")
}

// FailJob records a terminal error without retrying the idempotency key.
func (repository *CalcRepository) FailJob(ctx context.Context, jobID string) error {
	if repository == nil || repository.db == nil {
		return errors.New("calc database is required")
	}
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_calc_jobs
		SET status = 'ERROR', updated_at = UTC_TIMESTAMP(3)
		WHERE job_id = ? AND status = 'CALC'
	`, jobID)
	if err != nil {
		return fmt.Errorf("fail calc job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("fail calc job: read affected rows: %w", err)
	}
	if affected > 1 {
		return fmt.Errorf("fail calc job: affected rows = %d, want at most 1", affected)
	}
	return nil
}

// CalcMaterializedRecord is the durable status/value projection for one cell.
type CalcMaterializedRecord struct {
	RecordID    string
	FieldID     string
	ValueJSON   []byte
	FormulaHash string
	Status      string
	UpdatedAt   time.Time
}

// SaveMaterialized upserts one calc cell inside the caller's transaction.
func (repository *CalcRepository) SaveMaterialized(
	ctx context.Context,
	tx *sql.Tx,
	record CalcMaterializedRecord,
) error {
	if tx == nil {
		return errors.New("calc transaction is required")
	}
	if record.RecordID == "" || record.FieldID == "" {
		return errors.New("calc record_id and field_id are required")
	}
	if !validMaterializedStatus(record.Status) {
		return fmt.Errorf("invalid materialized status %q", record.Status)
	}
	var valueJSON any
	if len(record.ValueJSON) > 0 {
		if !json.Valid(record.ValueJSON) {
			return errors.New("materialized value_json must be valid JSON")
		}
		valueJSON = record.ValueJSON
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO excel_calc_materialized (
			record_id, field_id, value_json, formula_hash, status, updated_at
		) VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP(3))
		ON DUPLICATE KEY UPDATE
			value_json = VALUES(value_json),
			formula_hash = VALUES(formula_hash),
			status = VALUES(status),
			updated_at = UTC_TIMESTAMP(3)
	`, record.RecordID, record.FieldID, valueJSON, record.FormulaHash, record.Status)
	if err != nil {
		return fmt.Errorf("upsert calc materialization: %w", err)
	}
	return nil
}

// Status returns the materialization status, or IDLE when no row exists.
func (repository *CalcRepository) Status(
	ctx context.Context,
	recordID, fieldID string,
) (string, error) {
	if repository == nil || repository.db == nil {
		return "", errors.New("calc database is required")
	}
	if recordID == "" || fieldID == "" {
		return "", errors.New("calc record_id and field_id are required")
	}
	var status string
	err := repository.db.QueryRowContext(ctx, `
		SELECT status
		FROM excel_calc_materialized
		WHERE record_id = ? AND field_id = ?
	`, recordID, fieldID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "IDLE", nil
	}
	if err != nil {
		return "", fmt.Errorf("query calc materialization status: %w", err)
	}
	return status, nil
}

// JobStatus is used by operational checks and tests.
func (repository *CalcRepository) JobStatus(ctx context.Context, jobID string) (string, error) {
	if repository == nil || repository.db == nil {
		return "", errors.New("calc database is required")
	}
	var status string
	err := repository.db.QueryRowContext(ctx, `
		SELECT status FROM excel_calc_jobs WHERE job_id = ?
	`, jobID).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("query calc job status: %w", err)
	}
	return status, nil
}

func validCalcJobStatus(status string) bool {
	switch status {
	case "CALC", "SUCCESS", "ERROR":
		return true
	default:
		return false
	}
}

func validMaterializedStatus(status string) bool {
	switch status {
	case "IDLE", "CALC", "ERROR":
		return true
	default:
		return false
	}
}
