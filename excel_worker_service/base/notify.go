// base/notify.go：站内通知与保留期查询的数据访问。
package base

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	// NotifyTable stores one row per recipient notification.
	NotifyTable = "excel_notify_notifications"
)

var (
	// ErrNotifyNotFound reports a missing notification.
	ErrNotifyNotFound = errors.New("notification not found")
	// ErrNotifyForbidden reports a notification owned by another user.
	ErrNotifyForbidden = errors.New("notification owner mismatch")
	// ErrNotifyUserNotFound reports an invite username without an account.
	ErrNotifyUserNotFound = errors.New("notification user not found")
)

// Notification is the persistence projection of one in-app notification.
type Notification struct {
	ID         string
	UserID     string
	Type       string
	BaseID     string
	RecordID   string
	CommentID  string
	PayloadMap []byte
	Read       bool
	CreatedAt  int64
}

// NotificationCursor identifies the last notification in a page.
type NotificationCursor struct {
	CreatedAt int64
	ID        string
}

// NotifyRepository owns SQL access to excel_notify_notifications.
type NotifyRepository struct {
	db *sql.DB
}

// NewNotifyRepository creates the notify persistence gateway.
func NewNotifyRepository(db *sql.DB) *NotifyRepository {
	return &NotifyRepository{db: db}
}

// BeginTx starts one read-state mutation transaction.
func (repository *NotifyRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("notify database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin notify transaction: %w", err)
	}
	return tx, nil
}

// Create inserts one notification row.
func (repository *NotifyRepository) Create(
	ctx context.Context,
	notification Notification,
) error {
	if repository == nil || repository.db == nil {
		return errors.New("notify database is required")
	}
	layout, err := loadNotifyColumnLayout(ctx, repository.db)
	if err != nil {
		return err
	}
	payload := notification.PayloadMap
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	_, err = repository.db.ExecContext(ctx, `
		INSERT INTO excel_notify_notifications (
			id, user_id, type, base_id, record_id, comment_id,
			`+layout.payloadColumn+`, `+layout.readColumn+`, created_at
		) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)
	`, notification.ID, notification.UserID, notification.Type,
		notification.BaseID, notification.RecordID, notification.CommentID,
		payload, notification.Read, notification.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

// Delete removes one notification as compensation for a failed side effect.
func (repository *NotifyRepository) Delete(ctx context.Context, notificationID string) error {
	if repository == nil || repository.db == nil {
		return errors.New("notify database is required")
	}
	if _, err := repository.db.ExecContext(
		ctx,
		`DELETE FROM excel_notify_notifications WHERE id = ?`,
		notificationID,
	); err != nil {
		return fmt.Errorf("delete notification: %w", err)
	}
	return nil
}

// List returns notifications newer than the retention cutoff, newest first.
func (repository *NotifyRepository) List(
	ctx context.Context,
	userID string,
	cutoff int64,
	cursor *NotificationCursor,
	limit int,
) ([]Notification, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("notify database is required")
	}
	if limit <= 0 {
		return nil, errors.New("notification page limit must be positive")
	}
	layout, err := loadNotifyColumnLayout(ctx, repository.db)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT id, user_id, type,
		       COALESCE(base_id, ''), COALESCE(record_id, ''), COALESCE(comment_id, ''),
		       COALESCE(` + layout.payloadColumn + `, JSON_OBJECT()), ` + layout.readColumn + `, created_at
		FROM excel_notify_notifications
		WHERE user_id = ? AND created_at >= ?`
	arguments := []any{userID, cutoff}
	if cursor != nil {
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		arguments = append(arguments, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	arguments = append(arguments, limit+1)
	rows, err := repository.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()
	notifications := make([]Notification, 0, limit+1)
	for rows.Next() {
		var notification Notification
		var payload []byte
		if err := rows.Scan(
			&notification.ID,
			&notification.UserID,
			&notification.Type,
			&notification.BaseID,
			&notification.RecordID,
			&notification.CommentID,
			&payload,
			&notification.Read,
			&notification.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		notification.PayloadMap = payload
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}
	return notifications, nil
}

// UnreadCount returns the exact retained unread count from MySQL.
func (repository *NotifyRepository) UnreadCount(
	ctx context.Context,
	userID string,
	cutoff int64,
) (int64, error) {
	if repository == nil || repository.db == nil {
		return 0, errors.New("notify database is required")
	}
	layout, err := loadNotifyColumnLayout(ctx, repository.db)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM excel_notify_notifications
		WHERE user_id = ? AND created_at >= ? AND `+layout.readColumn+` = 0
	`, userID, cutoff).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

// MarkReadTx marks one notification read and reports whether state changed.
func (repository *NotifyRepository) MarkReadTx(
	ctx context.Context,
	tx *sql.Tx,
	userID, notificationID string,
) (bool, error) {
	if tx == nil {
		return false, errors.New("notify transaction is required")
	}
	layout, err := loadNotifyColumnLayout(ctx, tx)
	if err != nil {
		return false, err
	}
	var ownerID string
	var read bool
	err = tx.QueryRowContext(ctx, `
		SELECT user_id, `+layout.readColumn+`
		FROM excel_notify_notifications
		WHERE id = ?
		FOR UPDATE
	`, notificationID).Scan(&ownerID, &read)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotifyNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock notification: %w", err)
	}
	if ownerID != userID {
		return false, ErrNotifyForbidden
	}
	if read {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_notify_notifications
		SET `+layout.readColumn+` = 1
		WHERE id = ? AND user_id = ? AND `+layout.readColumn+` = 0
	`, notificationID, userID)
	if err != nil {
		return false, fmt.Errorf("mark notification read: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read mark-notification result: %w", err)
	}
	return affected == 1, nil
}

// MarkAllReadTx marks every unread retained notification for one user.
func (repository *NotifyRepository) MarkAllReadTx(
	ctx context.Context,
	tx *sql.Tx,
	userID string,
	cutoff int64,
) (int64, error) {
	if tx == nil {
		return 0, errors.New("notify transaction is required")
	}
	layout, err := loadNotifyColumnLayout(ctx, tx)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_notify_notifications
		SET `+layout.readColumn+` = 1
		WHERE user_id = ? AND created_at >= ? AND `+layout.readColumn+` = 0
	`, userID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read mark-all result: %w", err)
	}
	return affected, nil
}

// UserIDByUsername resolves invite usernames using the shared MySQL instance.
func (repository *NotifyRepository) UserIDByUsername(
	ctx context.Context,
	username string,
) (string, error) {
	if repository == nil || repository.db == nil {
		return "", errors.New("notify database is required")
	}
	var userID string
	err := repository.db.QueryRowContext(ctx, `
		SELECT id FROM excel_core.excel_iam_users WHERE username = ?
	`, strings.TrimSpace(username)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotifyUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve invite user id: %w", err)
	}
	return userID, nil
}

type notifyColumnLayout struct {
	payloadColumn string
	readColumn    string
}

type notifyColumnQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadNotifyColumnLayout(
	ctx context.Context,
	queryer notifyColumnQueryer,
) (notifyColumnLayout, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = 'excel_notify_notifications'
	`)
	if err != nil {
		return notifyColumnLayout{}, fmt.Errorf("inspect notify columns: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]struct{})
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return notifyColumnLayout{}, fmt.Errorf("scan notify column: %w", err)
		}
		columns[column] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return notifyColumnLayout{}, fmt.Errorf("iterate notify columns: %w", err)
	}
	layout := notifyColumnLayout{}
	if _, ok := columns["payload_map"]; ok {
		layout.payloadColumn = "payload_map"
	} else if _, ok := columns["payload_json"]; ok {
		layout.payloadColumn = "payload_json"
	} else {
		return notifyColumnLayout{}, errors.New("notify payload column is missing")
	}
	if _, ok := columns["read"]; ok {
		layout.readColumn = "`read`"
	} else if _, ok := columns["is_read"]; ok {
		layout.readColumn = "is_read"
	} else {
		return notifyColumnLayout{}, errors.New("notify read column is missing")
	}
	return layout, nil
}
