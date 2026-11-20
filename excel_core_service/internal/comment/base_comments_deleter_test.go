package comment_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/internal/comment"
)

const (
	cleanupCommentBaseID   = "01J00000000000000000003001"
	cleanupCommentTableID  = "01J00000000000000000003002"
	cleanupOtherBaseID     = "01J00000000000000000003003"
	cleanupOtherTableID    = "01J00000000000000000003004"
	cleanupCommentRecordID = "01J00000000000000000003005"
	cleanupSoftRecordID    = "01J00000000000000000003006"
	cleanupOtherRecordID   = "01J00000000000000000003007"
	cleanupOrphanRecordID  = "01J00000000000000000003008"
	cleanupCommentUserID   = "01J00000000000000000003009"
)

// TestBaseCommentsDeleterRemovesOnlyAttributableComments locks the comment
// cascade contract: every comment whose record belongs to the Base (including
// soft-deleted records) is removed, while comments of other Bases and comments
// whose record can no longer be resolved are left untouched.
func TestBaseCommentsDeleterRemovesOnlyAttributableComments(t *testing.T) {
	db := openCommentCleanupDB(t)
	seedCommentCleanupRows(t, db)
	t.Cleanup(func() { deleteCommentCleanupRows(t, db) })

	deleter := comment.NewBaseCommentsDeleter(base.NewCommentRepository(db))
	if err := deleter.DeleteBaseComments(
		context.Background(),
		cleanupCommentBaseID,
	); err != nil {
		t.Fatalf("DeleteBaseComments() error = %v", err)
	}
	for _, retained := range []struct {
		commentID string
		want      int
	}{
		{commentID: "01J00000000000000000003021", want: 0}, // Base record
		{commentID: "01J00000000000000000003022", want: 0}, // reply
		{commentID: "01J00000000000000000003023", want: 0}, // soft-deleted record
		{commentID: "01J00000000000000000003024", want: 1}, // other Base
		{commentID: "01J00000000000000000003025", want: 1}, // orphan record
	} {
		if got := countCommentRows(t, db, retained.commentID); got != retained.want {
			t.Fatalf("comment %s rows after cleanup = %d, want %d",
				retained.commentID, got, retained.want)
		}
	}

	if err := deleter.DeleteBaseComments(
		context.Background(),
		cleanupCommentBaseID,
	); err != nil {
		t.Fatalf("repeated DeleteBaseComments() error = %v", err)
	}
}

func openCommentCleanupDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testmysql.OpenTestDB(t, commentTestMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUpWithRetry(db); err != nil {
		t.Fatalf("base.MigrateUpWithRetry() error = %v", err)
	}
	deleteCommentCleanupRows(t, db)
	return db
}

func seedCommentCleanupRows(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO excel_structure_tables
				(id, enterprise_id, base_id, name, position, created_at)
				VALUES (?, ?, ?, 'cleanup-target', 0, ?)`,
			args: []any{
				cleanupCommentTableID, cleanupCommentUserID,
				cleanupCommentBaseID, now,
			},
		},
		{
			query: `INSERT INTO excel_structure_tables
				(id, enterprise_id, base_id, name, position, created_at)
				VALUES (?, ?, ?, 'cleanup-other', 0, ?)`,
			args: []any{
				cleanupOtherTableID, cleanupCommentUserID,
				cleanupOtherBaseID, now,
			},
		},
		{
			query: `INSERT INTO excel_records
				(id, table_id, cells_json, created_by, created_at, version)
				VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)`,
			args: []any{
				cleanupCommentRecordID, cleanupCommentTableID,
				cleanupCommentUserID, now,
			},
		},
		{
			query: `INSERT INTO excel_records
				(id, table_id, cells_json, created_by, created_at, version, soft_deleted_at)
				VALUES (?, ?, JSON_OBJECT(), ?, ?, 2, ?)`,
			args: []any{
				cleanupSoftRecordID, cleanupCommentTableID,
				cleanupCommentUserID, now, now,
			},
		},
		{
			query: `INSERT INTO excel_records
				(id, table_id, cells_json, created_by, created_at, version)
				VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)`,
			args: []any{
				cleanupOtherRecordID, cleanupOtherTableID,
				cleanupCommentUserID, now,
			},
		},
	}
	statements = append(statements,
		commentCleanupStatement(
			"01J00000000000000000003021", cleanupCommentRecordID, "", now,
		),
		commentCleanupStatement(
			"01J00000000000000000003022", cleanupCommentRecordID,
			"01J00000000000000000003021", now,
		),
		commentCleanupStatement(
			"01J00000000000000000003023", cleanupSoftRecordID, "", now,
		),
		commentCleanupStatement(
			"01J00000000000000000003024", cleanupOtherRecordID, "", now,
		),
		commentCleanupStatement(
			"01J00000000000000000003025", cleanupOrphanRecordID, "", now,
		),
	)
	for _, statement := range statements {
		if _, err := db.ExecContext(
			context.Background(),
			statement.query,
			statement.args...,
		); err != nil {
			t.Fatalf("seed comment cleanup rows with %q: %v", statement.query, err)
		}
	}
}

func commentCleanupStatement(
	commentID, recordID, parentID string,
	now int64,
) struct {
	query string
	args  []any
} {
	var parent any
	if parentID != "" {
		parent = parentID
	}
	return struct {
		query string
		args  []any
	}{
		query: `INSERT INTO excel_comment_comments
			(id, record_id, parent_id, content, author, status, mentions, created_at)
			VALUES (?, ?, ?, 'cleanup', ?, 'OPEN', JSON_ARRAY(), ?)`,
		args: []any{
			commentID, recordID, parent, cleanupCommentUserID, now,
		},
	}
}

func deleteCommentCleanupRows(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM excel_comment_comments WHERE id LIKE '01J0000000000000000000302%'`,
		},
		{
			query: `DELETE FROM excel_records WHERE id IN (?, ?, ?)`,
			args: []any{
				cleanupCommentRecordID,
				cleanupSoftRecordID,
				cleanupOtherRecordID,
			},
		},
		{
			query: `DELETE FROM excel_structure_tables WHERE id IN (?, ?)`,
			args:  []any{cleanupCommentTableID, cleanupOtherTableID},
		},
	} {
		if _, err := db.ExecContext(
			context.Background(),
			statement.query,
			statement.args...,
		); err != nil {
			t.Fatalf("delete comment cleanup rows with %q: %v", statement.query, err)
		}
	}
}

func countCommentRows(t *testing.T, db *sql.DB, commentID string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM excel_comment_comments WHERE id = ?`,
		commentID,
	).Scan(&count); err != nil {
		t.Fatalf("count comment %s: %v", commentID, err)
	}
	return count
}
