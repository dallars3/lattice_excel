// base/tasks.go：任务表领取/租约（FOR UPDATE SKIP LOCKED）。
package base

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskStatus is the persisted scheduler task state.
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending"
	TaskStatusRunning TaskStatus = "running"
	TaskStatusSuccess TaskStatus = "success"
	TaskStatusFailed  TaskStatus = "failed"
	TaskStatusDead    TaskStatus = "dead"
)

// SchedulerTask is one durable delayed task.
type SchedulerTask struct {
	ID         string
	Type       string
	Kind       string
	EntityID   string
	EventID    string
	Status     TaskStatus
	RunAt      int64
	RetryCount int
	MaxRetries int
	LeaseUntil int64
	OwnerPID   string
	Payload    []byte
	LastError  string
	CreatedAt  int64
	UpdatedAt  int64
}

// TaskRepository owns scheduler task claims and terminal transitions.
type TaskRepository struct {
	db *sql.DB
}

// NewTaskRepository creates the scheduler persistence gateway.
func NewTaskRepository(db *sql.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

// Enqueue inserts one pending task.
func (repository *TaskRepository) Enqueue(ctx context.Context, task SchedulerTask) error {
	if repository == nil || repository.db == nil {
		return errors.New("scheduler database is required")
	}
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Type) == "" || task.RunAt <= 0 {
		return errors.New("scheduler task id, type and run_at are required")
	}
	if task.MaxRetries <= 0 {
		task.MaxRetries = 5
	}
	if task.CreatedAt <= 0 {
		task.CreatedAt = time.Now().UTC().UnixMilli()
	}
	if task.UpdatedAt <= 0 {
		task.UpdatedAt = task.CreatedAt
	}
	payload := task.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	if !json.Valid(payload) {
		return errors.New("scheduler task payload must be valid JSON")
	}
	if _, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_scheduler_tasks (
			id, type, kind, entity_id, event_id, status, run_at, retry_count, max_retries,
			lease_until, owner_pid, payload, last_error, created_at, updated_at
		) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), 'pending', ?, 0, ?, NULL, NULL, ?, NULL, ?, ?)
	`, task.ID, task.Type, task.Kind, task.EntityID, task.EventID, task.RunAt, task.MaxRetries, payload, task.CreatedAt, task.UpdatedAt); err != nil {
		return fmt.Errorf("enqueue scheduler task: %w", err)
	}
	return nil
}

// EnqueueLifecycle inserts one pending lifecycle-incepted task, ignoring an
// already-pending task for the same (kind, entity_id) or an already-seen event.
func (repository *TaskRepository) EnqueueLifecycle(ctx context.Context, task SchedulerTask) (bool, error) {
	if repository == nil || repository.db == nil {
		return false, errors.New("scheduler database is required")
	}
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Type) == "" ||
		strings.TrimSpace(task.Kind) == "" || strings.TrimSpace(task.EntityID) == "" ||
		task.RunAt <= 0 {
		return false, errors.New("scheduler lifecycle task id, type, kind, entity_id and run_at are required")
	}
	if task.MaxRetries <= 0 {
		task.MaxRetries = 5
	}
	if task.CreatedAt <= 0 {
		task.CreatedAt = time.Now().UTC().UnixMilli()
	}
	if task.UpdatedAt <= 0 {
		task.UpdatedAt = task.CreatedAt
	}
	payload := task.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	if !json.Valid(payload) {
		return false, errors.New("scheduler task payload must be valid JSON")
	}
	result, err := repository.db.ExecContext(ctx, `
		INSERT INTO excel_scheduler_tasks (
			id, type, kind, entity_id, event_id, status, run_at, retry_count, max_retries,
			lease_until, owner_pid, payload, last_error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'pending', ?, 0, ?, NULL, NULL, ?, NULL, ?, ?)
		ON DUPLICATE KEY UPDATE id = id
	`, task.ID, task.Type, task.Kind, task.EntityID, task.EventID, task.RunAt, task.MaxRetries, payload, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("enqueue lifecycle scheduler task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("enqueue lifecycle scheduler task: read affected rows: %w", err)
	}
	return affected == 1, nil
}

// ClaimDue leases due or expired tasks in one transaction.
func (repository *TaskRepository) ClaimDue(
	ctx context.Context,
	now time.Time,
	ownerPID string,
	lease time.Duration,
	limit int,
) ([]SchedulerTask, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("scheduler database is required")
	}
	if strings.TrimSpace(ownerPID) == "" || lease <= 0 || limit <= 0 {
		return nil, errors.New("scheduler owner, lease and limit are required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin scheduler claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	nowMillis := now.UTC().UnixMilli()
	rows, err := tx.QueryContext(ctx, `
		SELECT id, type, status, run_at, retry_count, max_retries,
		       COALESCE(lease_until, 0), COALESCE(owner_pid, ''),
		       payload, COALESCE(last_error, ''), created_at, updated_at
		FROM excel_scheduler_tasks
		WHERE run_at <= ?
		  AND (
		    status IN ('pending', 'failed')
		    OR (status = 'running' AND lease_until IS NOT NULL AND lease_until <= ?)
		  )
		ORDER BY run_at, id
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, nowMillis, nowMillis, limit)
	if err != nil {
		return nil, fmt.Errorf("claim scheduler tasks: %w", err)
	}
	tasks := make([]SchedulerTask, 0, limit)
	for rows.Next() {
		task, scanErr := scanSchedulerTask(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		tasks = append(tasks, task)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close scheduler claim rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scheduler claim rows: %w", err)
	}
	leaseUntil := now.Add(lease).UTC().UnixMilli()
	for index := range tasks {
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE excel_scheduler_tasks
			SET status = 'running', lease_until = ?, owner_pid = ?, updated_at = ?
			WHERE id = ?
		`, leaseUntil, ownerPID, nowMillis, tasks[index].ID)
		if updateErr != nil {
			return nil, fmt.Errorf("lease scheduler task: %w", updateErr)
		}
		affected, affectedErr := result.RowsAffected()
		if affectedErr != nil {
			return nil, fmt.Errorf("read scheduler lease result: %w", affectedErr)
		}
		if affected != 1 {
			return nil, fmt.Errorf("lease scheduler task %s: affected rows = %d", tasks[index].ID, affected)
		}
		tasks[index].Status = TaskStatusRunning
		tasks[index].LeaseUntil = leaseUntil
		tasks[index].OwnerPID = ownerPID
		tasks[index].UpdatedAt = nowMillis
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit scheduler claim: %w", err)
	}
	return tasks, nil
}

// MarkSucceeded completes one leased task.
func (repository *TaskRepository) MarkSucceeded(
	ctx context.Context,
	task SchedulerTask,
	now time.Time,
) (SchedulerTask, error) {
	if repository == nil || repository.db == nil {
		return SchedulerTask{}, errors.New("scheduler database is required")
	}
	nowMillis := now.UTC().UnixMilli()
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_scheduler_tasks
		SET status = 'success', lease_until = NULL, owner_pid = NULL,
		    last_error = NULL, updated_at = ?
		WHERE id = ? AND status = 'running'
	`, nowMillis, task.ID)
	if err != nil {
		return SchedulerTask{}, fmt.Errorf("mark scheduler task succeeded: %w", err)
	}
	if err := requireSchedulerTaskRow(result); err != nil {
		return SchedulerTask{}, err
	}
	task.Status = TaskStatusSuccess
	task.LeaseUntil = 0
	task.OwnerPID = ""
	task.LastError = ""
	task.UpdatedAt = nowMillis
	return task, nil
}

// MarkFailed schedules exponential backoff or marks the task dead.
func (repository *TaskRepository) MarkFailed(
	ctx context.Context,
	task SchedulerTask,
	now time.Time,
	initialDelay, maxDelay time.Duration,
	taskErr error,
) (SchedulerTask, error) {
	if repository == nil || repository.db == nil {
		return SchedulerTask{}, errors.New("scheduler database is required")
	}
	retryCount := task.RetryCount + 1
	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}
	status := TaskStatusFailed
	runAt := now.Add(RetryDelay(initialDelay, maxDelay, retryCount)).UTC()
	if retryCount >= maxRetries {
		status = TaskStatusDead
		runAt = now.UTC()
	}
	lastError := ""
	if taskErr != nil {
		lastError = taskErr.Error()
	}
	result, err := repository.db.ExecContext(ctx, `
		UPDATE excel_scheduler_tasks
		SET status = ?, run_at = ?, retry_count = ?, max_retries = ?,
		    lease_until = NULL, owner_pid = NULL, last_error = ?, updated_at = ?
		WHERE id = ? AND status = 'running'
	`, status, runAt.UnixMilli(), retryCount, maxRetries, lastError, now.UTC().UnixMilli(), task.ID)
	if err != nil {
		return SchedulerTask{}, fmt.Errorf("mark scheduler task failed: %w", err)
	}
	if err := requireSchedulerTaskRow(result); err != nil {
		return SchedulerTask{}, err
	}
	task.Status = status
	task.RunAt = runAt.UnixMilli()
	task.RetryCount = retryCount
	task.MaxRetries = maxRetries
	task.LeaseUntil = 0
	task.OwnerPID = ""
	task.LastError = lastError
	task.UpdatedAt = now.UTC().UnixMilli()
	return task, nil
}

// Get reads one scheduler task for diagnostics and tests.
func (repository *TaskRepository) Get(ctx context.Context, taskID string) (SchedulerTask, error) {
	if repository == nil || repository.db == nil {
		return SchedulerTask{}, errors.New("scheduler database is required")
	}
	return scanSchedulerTask(repository.db.QueryRowContext(ctx, `
		SELECT id, type, status, run_at, retry_count, max_retries,
		       COALESCE(lease_until, 0), COALESCE(owner_pid, ''),
		       payload, COALESCE(last_error, ''), created_at, updated_at
		FROM excel_scheduler_tasks WHERE id = ?
	`, taskID))
}

// RetryDelay returns exponential backoff capped at maxDelay.
func RetryDelay(initial, maximum time.Duration, retryCount int) time.Duration {
	if initial <= 0 {
		initial = time.Second
	}
	if maximum < initial {
		maximum = initial
	}
	delay := initial
	for attempt := 1; attempt < retryCount; attempt++ {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

type schedulerTaskScanner interface {
	Scan(...any) error
}

func scanSchedulerTask(scanner schedulerTaskScanner) (SchedulerTask, error) {
	var task SchedulerTask
	var status string
	if err := scanner.Scan(
		&task.ID,
		&task.Type,
		&status,
		&task.RunAt,
		&task.RetryCount,
		&task.MaxRetries,
		&task.LeaseUntil,
		&task.OwnerPID,
		&task.Payload,
		&task.LastError,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		return SchedulerTask{}, fmt.Errorf("scan scheduler task: %w", err)
	}
	task.Status = TaskStatus(status)
	return task, nil
}

func requireSchedulerTaskRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read scheduler task update result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("scheduler task state did not change: affected rows = %d", affected)
	}
	return nil
}
