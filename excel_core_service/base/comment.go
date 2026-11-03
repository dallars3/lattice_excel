// base/comment.go：两级评论的数据访问。
package base

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// CommentTable stores record comments and their one-level replies.
	CommentTable = "excel_comment_comments"
)

var (
	// ErrCommentNotFound reports a missing comment.
	ErrCommentNotFound = errors.New("comment not found")
	// ErrCommentRecordNotFound reports a missing or deleted record.
	ErrCommentRecordNotFound = errors.New("comment record not found")
)

// Comment is the persistence projection of one comment or reply.
type Comment struct {
	ID        string
	BaseID    string
	RecordID  string
	ParentID  string
	Content   string
	Author    string
	Status    string
	Mentions  []string
	CreatedAt int64
}

// CommentCursor identifies the last top-level thread returned on a page.
type CommentCursor struct {
	CreatedAt int64
	ID        string
}

// CommentRepository owns SQL access to excel_comment_comments.
type CommentRepository struct {
	db *sql.DB
}

// NewCommentRepository creates the comment persistence gateway.
func NewCommentRepository(db *sql.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

// BeginTx starts one comment mutation transaction.
func (repository *CommentRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("comment database is required")
	}
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin comment transaction: %w", err)
	}
	return tx, nil
}

// RecordBaseIDForUpdate resolves and locks the record's Base owner.
func (repository *CommentRepository) RecordBaseIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	recordID string,
) (string, error) {
	if tx == nil {
		return "", errors.New("comment transaction is required")
	}
	var baseID string
	err := tx.QueryRowContext(ctx, `
		SELECT tables.base_id
		FROM excel_records AS records
		JOIN excel_structure_tables AS tables ON tables.id = records.table_id
		WHERE records.id = ? AND records.soft_deleted_at IS NULL
		FOR UPDATE
	`, recordID).Scan(&baseID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCommentRecordNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve comment record base: %w", err)
	}
	return baseID, nil
}

// CreateTx inserts one comment in the caller's transaction.
func (repository *CommentRepository) CreateTx(
	ctx context.Context,
	tx *sql.Tx,
	comment Comment,
) error {
	if tx == nil {
		return errors.New("comment transaction is required")
	}
	layout, err := loadCommentColumnLayout(ctx, tx)
	if err != nil {
		return err
	}
	mentions, err := json.Marshal(comment.Mentions)
	if err != nil {
		return fmt.Errorf("marshal comment mentions: %w", err)
	}
	columns := []string{"id"}
	values := []any{comment.ID}
	if layout.hasBaseID {
		columns = append(columns, "base_id")
		values = append(values, comment.BaseID)
	}
	columns = append(columns,
		"record_id", "parent_id", "content", "author", "status",
		layout.mentionColumn, "created_at",
	)
	var parentID any
	if comment.ParentID != "" {
		parentID = comment.ParentID
	}
	values = append(values,
		comment.RecordID,
		parentID,
		comment.Content,
		comment.Author,
		layout.statusValue(comment.Status),
		mentions,
		comment.CreatedAt,
	)
	placeholders := make([]string, len(columns))
	for index := range placeholders {
		placeholders[index] = "?"
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO excel_comment_comments (`+strings.Join(columns, ", ")+`)
		VALUES (`+strings.Join(placeholders, ", ")+`)
	`, values...)
	if err != nil {
		return fmt.Errorf("insert comment: %w", err)
	}
	return nil
}

// CommentByID loads one comment.
func (repository *CommentRepository) CommentByID(
	ctx context.Context,
	commentID string,
) (Comment, error) {
	if repository == nil || repository.db == nil {
		return Comment{}, errors.New("comment database is required")
	}
	layout, err := loadCommentColumnLayout(ctx, repository.db)
	if err != nil {
		return Comment{}, err
	}
	comment, err := scanComment(repository.db.QueryRowContext(ctx, `
		SELECT `+commentSelectColumns(layout)+`
		FROM excel_comment_comments
		WHERE id = ?
	`, commentID))
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, ErrCommentNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("query comment: %w", err)
	}
	return comment, nil
}

// CommentByIDForUpdate locks one comment for a mutation.
func (repository *CommentRepository) CommentByIDForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	commentID string,
) (Comment, error) {
	if tx == nil {
		return Comment{}, errors.New("comment transaction is required")
	}
	layout, err := loadCommentColumnLayout(ctx, tx)
	if err != nil {
		return Comment{}, err
	}
	comment, err := scanComment(tx.QueryRowContext(ctx, `
		SELECT `+commentSelectColumns(layout)+`
		FROM excel_comment_comments
		WHERE id = ?
		FOR UPDATE
	`, commentID))
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, ErrCommentNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("lock comment: %w", err)
	}
	return comment, nil
}

// TopLevelPage loads top-level threads newer than the cursor. It returns at
// most limit+1 rows so the caller can compute has_more.
func (repository *CommentRepository) TopLevelPage(
	ctx context.Context,
	recordID string,
	cursor *CommentCursor,
	limit int,
) ([]Comment, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("comment database is required")
	}
	if limit <= 0 {
		return nil, errors.New("comment page limit must be positive")
	}
	layout, err := loadCommentColumnLayout(ctx, repository.db)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT ` + commentSelectColumns(layout) + `
		FROM excel_comment_comments
		WHERE record_id = ? AND parent_id IS NULL`
	arguments := []any{recordID}
	if cursor != nil {
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		arguments = append(arguments, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	arguments = append(arguments, limit+1)
	rows, err := repository.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query top-level comments: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

// RepliesByParentIDs loads all replies for the given top-level thread ids.
func (repository *CommentRepository) RepliesByParentIDs(
	ctx context.Context,
	parentIDs []string,
) ([]Comment, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("comment database is required")
	}
	if len(parentIDs) == 0 {
		return []Comment{}, nil
	}
	layout, err := loadCommentColumnLayout(ctx, repository.db)
	if err != nil {
		return nil, err
	}
	placeholders := make([]string, len(parentIDs))
	arguments := make([]any, len(parentIDs))
	for index, parentID := range parentIDs {
		placeholders[index] = "?"
		arguments[index] = parentID
	}
	rows, err := repository.db.QueryContext(ctx, `
		SELECT `+commentSelectColumns(layout)+`
		FROM excel_comment_comments
		WHERE parent_id IN (`+strings.Join(placeholders, ", ")+`)
		ORDER BY created_at ASC, id ASC
	`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query comment replies: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

// UnsolvedCount counts unresolved top-level threads.
func (repository *CommentRepository) UnsolvedCount(
	ctx context.Context,
	recordID string,
) (int32, error) {
	if repository == nil || repository.db == nil {
		return 0, errors.New("comment database is required")
	}
	layout, err := loadCommentColumnLayout(ctx, repository.db)
	if err != nil {
		return 0, err
	}
	var count int32
	if err := repository.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM excel_comment_comments
		WHERE record_id = ? AND parent_id IS NULL AND status = ?
	`, recordID, layout.statusValue("OPEN")).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unsolved comments: %w", err)
	}
	return count, nil
}

// SetStatusTx updates one locked comment's status.
func (repository *CommentRepository) SetStatusTx(
	ctx context.Context,
	tx *sql.Tx,
	commentID, status string,
) error {
	if tx == nil {
		return errors.New("comment transaction is required")
	}
	layout, err := loadCommentColumnLayout(ctx, tx)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE excel_comment_comments SET status = ? WHERE id = ?
	`, layout.statusValue(status), commentID)
	if err != nil {
		return fmt.Errorf("update comment status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read comment status result: %w", err)
	}
	if affected != 1 {
		return ErrCommentNotFound
	}
	return nil
}

// DeleteCommentTx physically deletes one reply, or a top-level comment and its
// two-level replies.
func (repository *CommentRepository) DeleteCommentTx(
	ctx context.Context,
	tx *sql.Tx,
	comment Comment,
) error {
	if tx == nil {
		return errors.New("comment transaction is required")
	}
	query := `DELETE FROM excel_comment_comments WHERE id = ?`
	arguments := []any{comment.ID}
	if comment.ParentID == "" {
		query = `DELETE FROM excel_comment_comments WHERE id = ? OR parent_id = ?`
		arguments = append(arguments, comment.ID)
	}
	result, err := tx.ExecContext(ctx, query, arguments...)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read comment delete result: %w", err)
	}
	if affected == 0 {
		return ErrCommentNotFound
	}
	return nil
}

type commentScanner interface {
	Scan(dest ...any) error
}

type commentColumnLayout struct {
	mentionColumn string
	hasBaseID     bool
	numericStatus bool
}

func (layout commentColumnLayout) statusValue(status string) any {
	if !layout.numericStatus {
		return status
	}
	if status == "RESOLVED" {
		return 2
	}
	return 1
}

type commentColumnQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadCommentColumnLayout(
	ctx context.Context,
	queryer commentColumnQueryer,
) (commentColumnLayout, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = 'excel_comment_comments'
	`)
	if err != nil {
		return commentColumnLayout{}, fmt.Errorf("inspect comment columns: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return commentColumnLayout{}, fmt.Errorf("scan comment column: %w", err)
		}
		columns[name] = dataType
	}
	if err := rows.Err(); err != nil {
		return commentColumnLayout{}, fmt.Errorf("iterate comment columns: %w", err)
	}
	layout := commentColumnLayout{}
	if _, ok := columns["mentions"]; ok {
		layout.mentionColumn = "mentions"
	} else if _, ok := columns["mention_json"]; ok {
		layout.mentionColumn = "mention_json"
	} else {
		return commentColumnLayout{}, errors.New("comment mentions column is missing")
	}
	_, layout.hasBaseID = columns["base_id"]
	statusType := columns["status"]
	layout.numericStatus = statusType == "tinyint" || statusType == "smallint" ||
		statusType == "int" || statusType == "bigint"
	return layout, nil
}

func commentSelectColumns(layout commentColumnLayout) string {
	return `
		id, record_id, parent_id, content, author,
		CASE
			WHEN status = 'OPEN' THEN 'OPEN'
			WHEN status = 'RESOLVED' THEN 'RESOLVED'
			WHEN status = 1 THEN 'OPEN'
			WHEN status = 2 THEN 'RESOLVED'
			ELSE 'OPEN'
		END,
		` + layout.mentionColumn + `,
		created_at`
}

func scanComment(scanner commentScanner) (Comment, error) {
	var comment Comment
	var parentID sql.NullString
	var mentions []byte
	if err := scanner.Scan(
		&comment.ID,
		&comment.RecordID,
		&parentID,
		&comment.Content,
		&comment.Author,
		&comment.Status,
		&mentions,
		&comment.CreatedAt,
	); err != nil {
		return Comment{}, err
	}
	if parentID.Valid {
		comment.ParentID = parentID.String
	}
	if len(mentions) > 0 {
		if err := json.Unmarshal(mentions, &comment.Mentions); err != nil {
			return Comment{}, fmt.Errorf("decode comment mentions: %w", err)
		}
	}
	if comment.Mentions == nil {
		comment.Mentions = []string{}
	}
	return comment, nil
}

func scanComments(rows *sql.Rows) ([]Comment, error) {
	comments := make([]Comment, 0)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comments: %w", err)
	}
	return comments, nil
}
