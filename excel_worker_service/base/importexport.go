// base/importexport.go：对应 excel_importexport_tasks 表的数据操作。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	// ImportExportTaskTable stores durable import and export jobs.
	ImportExportTaskTable = "excel_importexport_tasks"
)

var (
	// ErrImportExportTaskNotFound reports a missing task row.
	ErrImportExportTaskNotFound = errors.New("importexport task not found")
)

// ImportExportTask is the persistence projection shared by imports and exports.
type ImportExportTask struct {
	ID              string
	Kind            string
	Status          string
	FileID          string
	BaseID          string
	TableID         string
	ViewID          string
	Format          string
	TotalRows       int32
	OKRows          int32
	FailRows        int32
	ReportFileID    string
	ClientRequestID string
	Payload         []byte
	CreatedAt       int64
	UpdatedAt       int64
}

// ImportExportRepository owns SQL access to excel_importexport_tasks.
type ImportExportRepository struct {
	db *sql.DB
}

// NewImportExportRepository creates the import/export task persistence gateway.
func NewImportExportRepository(db *sql.DB) *ImportExportRepository {
	return &ImportExportRepository{db: db}
}

// Create inserts one task. A duplicate client_request_id returns the existing row.
func (repository *ImportExportRepository) Create(
	ctx context.Context,
	task ImportExportTask,
) (ImportExportTask, error) {
	if repository == nil || repository.db == nil {
		return ImportExportTask{}, errors.New("importexport database is required")
	}
	payload := task.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	_, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_importexport_tasks (
			id, kind, status, file_id, base_id, table_id, view_id,
			format, total_rows, ok_rows, fail_rows, report_file_id,
			client_request_id, payload, created_at, updated_at
		) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''),
		          NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''),
		          NULLIF(?, ''), ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = id
	`, task.ID, task.Kind, task.Status, task.FileID, task.BaseID, task.TableID,
		task.ViewID, task.Format, task.TotalRows, task.OKRows, task.FailRows,
		task.ReportFileID, task.ClientRequestID, payload, task.CreatedAt,
		task.UpdatedAt)
	if err != nil {
		return ImportExportTask{}, fmt.Errorf("insert importexport task: %w", err)
	}
	if task.ClientRequestID != "" {
		existing, err := repository.GetByClientRequestID(ctx, task.ClientRequestID)
		if err != nil {
			return ImportExportTask{}, err
		}
		return existing, nil
	}
	return task, nil
}

// Get reads one task by identifier.
func (repository *ImportExportRepository) Get(
	ctx context.Context,
	taskID string,
) (ImportExportTask, error) {
	if repository == nil || repository.db == nil {
		return ImportExportTask{}, errors.New("importexport database is required")
	}
	row := repository.db.QueryRowContext(ctx, importExportTaskSelect+` WHERE id = ?`, taskID)
	task, err := scanImportExportTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportExportTask{}, ErrImportExportTaskNotFound
	}
	if err != nil {
		return ImportExportTask{}, fmt.Errorf("read importexport task: %w", err)
	}
	return task, nil
}

// GetByClientRequestID reads the idempotent replay result.
func (repository *ImportExportRepository) GetByClientRequestID(
	ctx context.Context,
	clientRequestID string,
) (ImportExportTask, error) {
	if repository == nil || repository.db == nil {
		return ImportExportTask{}, errors.New("importexport database is required")
	}
	row := repository.db.QueryRowContext(
		ctx,
		importExportTaskSelect+` WHERE client_request_id = ?`,
		clientRequestID,
	)
	task, err := scanImportExportTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportExportTask{}, ErrImportExportTaskNotFound
	}
	if err != nil {
		return ImportExportTask{}, fmt.Errorf("read idempotent importexport task: %w", err)
	}
	return task, nil
}

// Update persists task state and counters.
func (repository *ImportExportRepository) Update(
	ctx context.Context,
	task ImportExportTask,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("importexport database is required")
	}
	payload := task.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_importexport_tasks
		SET status = ?, file_id = NULLIF(?, ''), base_id = NULLIF(?, ''),
		    table_id = NULLIF(?, ''),
		    format = NULLIF(?, ''), total_rows = ?, ok_rows = ?, fail_rows = ?,
		    report_file_id = NULLIF(?, ''), payload = ?, updated_at = ?
		WHERE id = ?
	`, task.Status, task.FileID, task.BaseID, task.TableID, task.Format,
		task.TotalRows, task.OKRows, task.FailRows, task.ReportFileID, payload,
		task.UpdatedAt, task.ID)
	if err != nil {
		return fmt.Errorf("update importexport task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read importexport task update result: %w", err)
	}
	if affected == 0 {
		return ErrImportExportTaskNotFound
	}
	return nil
}

// FileIDsByBaseIDs discovers generated files that must be removed for E-17.
func (repository *ImportExportRepository) FileIDsByBaseIDs(
	ctx context.Context,
	baseIDs []string,
) ([]string, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("importexport database is required")
	}
	if len(baseIDs) == 0 {
		return nil, nil
	}
	rows, err := repository.db.QueryContext(ctx, `
		SELECT COALESCE(file_id, ''), COALESCE(report_file_id, '')
		FROM excel_importexport_tasks
		WHERE base_id IN (`+sqlPlaceholders(len(baseIDs))+`)
		  AND status <> 'cleaned'
	`, stringArguments(baseIDs)...)
	if err != nil {
		return nil, fmt.Errorf("query enterprise import/export files: %w", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	var fileIDs []string
	for rows.Next() {
		var fileID, reportFileID string
		if err := rows.Scan(&fileID, &reportFileID); err != nil {
			return nil, fmt.Errorf("scan enterprise import/export files: %w", err)
		}
		for _, candidate := range []string{fileID, reportFileID} {
			if candidate == "" {
				continue
			}
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			fileIDs = append(fileIDs, candidate)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enterprise import/export files: %w", err)
	}
	return fileIDs, nil
}

// MarkCleanedByBaseIDs retires import/export tasks after their files are removed.
func (repository *ImportExportRepository) MarkCleanedByBaseIDs(
	ctx context.Context,
	baseIDs []string,
	updatedAt int64,
) (int64, error) {
	if repository == nil || repository.db == nil {
		return 0, errors.New("importexport database is required")
	}
	if len(baseIDs) == 0 {
		return 0, nil
	}
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_importexport_tasks
		SET status = 'cleaned', file_id = NULL, report_file_id = NULL,
		    payload = JSON_OBJECT(), updated_at = ?
		WHERE base_id IN (`+sqlPlaceholders(len(baseIDs))+`)
		  AND status <> 'cleaned'
	`, append([]any{updatedAt}, stringArguments(baseIDs)...)...)
	if err != nil {
		return 0, fmt.Errorf("mark enterprise import/export tasks cleaned: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read cleaned import/export task count: %w", err)
	}
	return affected, nil
}

func stringArguments(values []string) []any {
	arguments := make([]any, 0, len(values))
	for _, value := range values {
		arguments = append(arguments, value)
	}
	return arguments
}

const importExportTaskSelect = `
	SELECT id, kind, status, COALESCE(file_id, ''), COALESCE(base_id, ''),
	       COALESCE(table_id, ''), COALESCE(view_id, ''), COALESCE(format, ''),
	       total_rows, ok_rows, fail_rows, COALESCE(report_file_id, ''),
	       COALESCE(client_request_id, ''), payload, created_at, updated_at
	FROM excel_importexport_tasks`

type importExportTaskScanner interface {
	Scan(...any) error
}

func scanImportExportTask(scanner importExportTaskScanner) (ImportExportTask, error) {
	var task ImportExportTask
	err := scanner.Scan(
		&task.ID,
		&task.Kind,
		&task.Status,
		&task.FileID,
		&task.BaseID,
		&task.TableID,
		&task.ViewID,
		&task.Format,
		&task.TotalRows,
		&task.OKRows,
		&task.FailRows,
		&task.ReportFileID,
		&task.ClientRequestID,
		&task.Payload,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	return task, err
}
