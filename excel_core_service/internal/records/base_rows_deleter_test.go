package records_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/internal/records"
)

const (
	cleanupBaseID          = "01J00000000000000000002001"
	cleanupTableID         = "01J00000000000000000002002"
	cleanupOtherTableID    = "01J00000000000000000002003"
	cleanupRecordID        = "01J00000000000000000002004"
	cleanupDeletedRecordID = "01J00000000000000000002005"
	cleanupOtherRecordID   = "01J00000000000000000002006"
	cleanupFieldID         = "01J00000000000000000002007"
	cleanupUserID          = "01J00000000000000000002008"
)

// TestBaseRowsDeleterRemovesRecordsHistoriesCalcAndOplog locks the cascade
// cleanup contract: deleting a Base removes every record-owned row for its
// tables and only the oplog entries attributable to those records.
func TestBaseRowsDeleterRemovesRecordsHistoriesCalcAndOplog(t *testing.T) {
	db := openCleanupDB(t)
	seedCleanupRows(t, db)
	t.Cleanup(func() { deleteCleanupRows(t, db) })

	deleter := records.NewBaseRowsDeleter(base.NewRecordsRepository(db))
	if err := deleter.DeleteBaseRows(
		context.Background(),
		cleanupBaseID,
		[]string{cleanupTableID},
	); err != nil {
		t.Fatalf("DeleteBaseRows() error = %v", err)
	}
	for _, check := range []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "records",
			query: `SELECT COUNT(*) FROM excel_records WHERE table_id = ?`,
			args:  []any{cleanupTableID},
		},
		{
			name:  "histories",
			query: `SELECT COUNT(*) FROM excel_records_histories WHERE record_id IN (?, ?)`,
			args:  []any{cleanupRecordID, cleanupDeletedRecordID},
		},
		{
			name:  "calc materialized",
			query: `SELECT COUNT(*) FROM excel_calc_materialized WHERE record_id = ?`,
			args:  []any{cleanupRecordID},
		},
		{
			name:  "calc jobs",
			query: `SELECT COUNT(*) FROM excel_calc_jobs WHERE table_id = ?`,
			args:  []any{cleanupTableID},
		},
		{
			name: "attributable oplog",
			query: `SELECT COUNT(*) FROM excel_records_oplog
				WHERE client_request_id IN (?, ?, ?)`,
			args: []any{
				"cleanup-table-oplog",
				"cleanup-record-oplog",
				"cleanup-list-oplog",
			},
		},
	} {
		if got := countRows(t, db, check.query, check.args...); got != 0 {
			t.Fatalf("%s rows after cleanup = %d, want 0", check.name, got)
		}
	}
	for _, retained := range []struct {
		name  string
		query string
		args  []any
		want  int
	}{
		{
			name:  "other table records",
			query: `SELECT COUNT(*) FROM excel_records WHERE table_id = ?`,
			args:  []any{cleanupOtherTableID},
			want:  1,
		},
		{
			name:  "other table histories",
			query: `SELECT COUNT(*) FROM excel_records_histories WHERE record_id = ?`,
			args:  []any{cleanupOtherRecordID},
			want:  1,
		},
		{
			name:  "other table calc materialized",
			query: `SELECT COUNT(*) FROM excel_calc_materialized WHERE record_id = ?`,
			args:  []any{cleanupOtherRecordID},
			want:  1,
		},
		{
			name:  "other table calc jobs",
			query: `SELECT COUNT(*) FROM excel_calc_jobs WHERE table_id = ?`,
			args:  []any{cleanupOtherTableID},
			want:  1,
		},
		{
			name: "unattributable oplog",
			query: `SELECT COUNT(*) FROM excel_records_oplog
				WHERE client_request_id IN (?, ?, ?)`,
			args: []any{
				"cleanup-other-table-oplog",
				"cleanup-other-record-oplog",
				"cleanup-unknown-record-oplog",
			},
			want: 3,
		},
	} {
		if got := countRows(t, db, retained.query, retained.args...); got != retained.want {
			t.Fatalf("%s rows after cleanup = %d, want %d",
				retained.name, got, retained.want)
		}
	}

	if err := deleter.DeleteBaseRows(
		context.Background(),
		cleanupBaseID,
		[]string{cleanupTableID},
	); err != nil {
		t.Fatalf("repeated DeleteBaseRows() error = %v", err)
	}
}

func openCleanupDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testmysql.OpenTestDB(t, recordsTestMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	deleteCleanupRows(t, db)
	return db
}

func seedCleanupRows(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO excel_records
				(id, table_id, cells_json, created_by, created_at, version)
				VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)`,
			args: []any{cleanupRecordID, cleanupTableID, cleanupUserID, now},
		},
		{
			query: `INSERT INTO excel_records
				(id, table_id, cells_json, created_by, created_at, version, soft_deleted_at)
				VALUES (?, ?, JSON_OBJECT(), ?, ?, 2, ?)`,
			args: []any{
				cleanupDeletedRecordID,
				cleanupTableID,
				cleanupUserID,
				now,
				now,
			},
		},
		{
			query: `INSERT INTO excel_records
				(id, table_id, cells_json, created_by, created_at, version)
				VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)`,
			args: []any{cleanupOtherRecordID, cleanupOtherTableID, cleanupUserID, now},
		},
		{
			query: "INSERT INTO excel_records_histories\n" +
				"(id, record_id, field_id, `before`, `after`, operator, created_at)\n" +
				"VALUES (?, ?, ?, JSON_OBJECT(), JSON_OBJECT(), ?, ?)",
			args: []any{
				"01J00000000000000000002011", cleanupRecordID, cleanupFieldID,
				cleanupUserID, now,
			},
		},
		{
			query: "INSERT INTO excel_records_histories\n" +
				"(id, record_id, field_id, `before`, `after`, operator, created_at)\n" +
				"VALUES (?, ?, ?, JSON_OBJECT(), JSON_OBJECT(), ?, ?)",
			args: []any{
				"01J00000000000000000002012", cleanupOtherRecordID, cleanupFieldID,
				cleanupUserID, now,
			},
		},
		{
			query: `INSERT INTO excel_calc_materialized
				(record_id, field_id, value_json, formula_hash, status, reason)
				VALUES (?, ?, NULL, '', 'IDLE', '')`,
			args: []any{cleanupRecordID, cleanupFieldID},
		},
		{
			query: `INSERT INTO excel_calc_materialized
				(record_id, field_id, value_json, formula_hash, status, reason)
				VALUES (?, ?, NULL, '', 'IDLE', '')`,
			args: []any{cleanupOtherRecordID, cleanupFieldID},
		},
		{
			query: `INSERT INTO excel_calc_jobs
				(job_id, table_id, status, payload, attempts)
				VALUES (?, ?, 'CALC', JSON_OBJECT(), 1)`,
			args: []any{"01J00000000000000000002021", cleanupTableID},
		},
		{
			query: `INSERT INTO excel_calc_jobs
				(job_id, table_id, status, payload, attempts)
				VALUES (?, ?, 'CALC', JSON_OBJECT(), 1)`,
			args: []any{"01J00000000000000000002022", cleanupOtherTableID},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES ('cleanup-table-oplog', 'create_record', ?, JSON_OBJECT(), ?)`,
			args: []any{
				`{"table_id":"` + cleanupTableID + `"}`,
				now,
			},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES ('cleanup-record-oplog', 'update_cell', ?, JSON_OBJECT(), ?)`,
			args: []any{
				`{"record_id":"` + cleanupRecordID + `"}`,
				now,
			},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES ('cleanup-list-oplog', 'delete_records', ?, JSON_OBJECT(), ?)`,
			args: []any{
				`{"record_id_list":["` + cleanupRecordID + `","` +
					cleanupDeletedRecordID + `"]}`,
				now,
			},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES ('cleanup-other-table-oplog', 'create_record', ?, JSON_OBJECT(), ?)`,
			args: []any{
				`{"table_id":"` + cleanupOtherTableID + `"}`,
				now,
			},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES ('cleanup-other-record-oplog', 'update_cell', ?, JSON_OBJECT(), ?)`,
			args: []any{
				`{"record_id":"` + cleanupOtherRecordID + `"}`,
				now,
			},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES ('cleanup-unknown-record-oplog', 'update_cell', ?, JSON_OBJECT(), ?)`,
			args: []any{
				`{"record_id":"01J00000000000000000002999"}`,
				now,
			},
		},
	} {
		if _, err := db.ExecContext(
			context.Background(),
			statement.query,
			statement.args...,
		); err != nil {
			t.Fatalf("seed cleanup rows with %q: %v", statement.query, err)
		}
	}
}

func deleteCleanupRows(t *testing.T, db *sql.DB) {
	t.Helper()
	recordIDs := []any{
		cleanupRecordID,
		cleanupDeletedRecordID,
		cleanupOtherRecordID,
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM excel_records_oplog
				WHERE client_request_id LIKE 'cleanup-%'`,
		},
		{
			query: `DELETE FROM excel_records_histories
				WHERE record_id IN (?, ?, ?)`,
			args: recordIDs,
		},
		{
			query: `DELETE FROM excel_calc_materialized
				WHERE record_id IN (?, ?, ?)`,
			args: recordIDs,
		},
		{
			query: `DELETE FROM excel_calc_jobs WHERE table_id IN (?, ?)`,
			args:  []any{cleanupTableID, cleanupOtherTableID},
		},
		{
			query: `DELETE FROM excel_records WHERE id IN (?, ?, ?)`,
			args:  recordIDs,
		},
	} {
		if _, err := db.ExecContext(
			context.Background(),
			statement.query,
			statement.args...,
		); err != nil {
			t.Fatalf("delete cleanup rows with %q: %v", statement.query, err)
		}
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(
		context.Background(),
		query,
		args...,
	).Scan(&count); err != nil {
		t.Fatalf("count rows with %q: %v", query, err)
	}
	return count
}
