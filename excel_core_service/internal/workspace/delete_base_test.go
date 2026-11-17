package workspace_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/base"
	"excel_core_service/internal/structure"
)

const (
	deleteBaseRecordID      = "01J00000000000000000000210"
	deleteBaseDeletedRecID  = "01J00000000000000000000211"
	deleteBaseHistoryID     = "01J00000000000000000000212"
	deleteBaseCommentID     = "01J00000000000000000000213"
	deleteBaseReplyID       = "01J00000000000000000000214"
	deleteBaseFieldID       = "01J00000000000000000000215"
	deleteBaseLinkToken     = "delete-base-cascade-token"
	deleteBaseOplogID       = "delete-base-oplog"
	deleteBaseRecordOplogID = "delete-base-record-oplog"
	deleteBaseJobID         = "01J00000000000000000000216"
)

func TestDeleteBaseCascadesAndCleansEveryOwnedRow(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "待删除",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	baseID := created.GetBase().GetId()
	tables, err := harness.structure.TablesByBase(
		context.Background(),
		testEnterprise,
		baseID,
	)
	if err != nil {
		t.Fatalf("TablesByBase() error = %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("created Base tables = %d, want 1", len(tables))
	}
	seedDeleteBaseCascade(t, harness.db, baseID, tables[0].ID)

	if _, err := harness.client.DeleteBase(ctx, &workspacev1.DeleteBaseRequest{
		BaseId:  baseID,
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteBase() error = %v", err)
	}
	assertDeleteBaseRowsGone(t, harness.db, baseID, tables[0].ID)
	assertDeleteBaseAudit(t, harness)
	if _, err := harness.structure.TableByID(
		context.Background(),
		testEnterprise,
		tables[0].ID,
	); !errors.Is(err, base.ErrStructureNotFound) {
		t.Fatalf("TableByID(after delete) error = %v, want deleted", err)
	}
	if _, err := harness.repository.BaseByID(context.Background(), baseID); !errors.Is(err, base.ErrWorkspaceNotFound) {
		t.Fatalf("BaseByID(after delete) error = %v, want not found", err)
	}
	home, err := harness.client.GetHome(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetHome() error = %v", err)
	}
	for _, group := range [][]*workspacev1.Base{
		home.GetHome().GetRecentList(),
		home.GetHome().GetCreatedList(),
		home.GetHome().GetSharedList(),
		home.GetHome().GetStarList(),
	} {
		for _, item := range group {
			if item.GetId() == baseID {
				t.Fatalf("deleted Base still present in home lists")
			}
		}
	}
}

func TestDeleteBaseKeepsBaseWhenCleanupFails(t *testing.T) {
	harness := newWorkspaceHarnessWith(
		t,
		&fakeRecordsDeleter{err: errors.New("records cleanup unavailable")},
		nil,
	)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "清理失败",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	baseID := created.GetBase().GetId()
	if _, err := harness.client.DeleteBase(ctx, &workspacev1.DeleteBaseRequest{
		BaseId:  baseID,
		Confirm: true,
	}); err == nil {
		t.Fatal("DeleteBase() succeeded, want cleanup error")
	}
	if _, err := harness.repository.BaseByID(context.Background(), baseID); err != nil {
		t.Fatalf("BaseByID() after failed cleanup error = %v, want Base retained", err)
	}
}

func TestDeleteBaseIsIdempotent(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "重复删除",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	baseID := created.GetBase().GetId()
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := harness.client.DeleteBase(ctx, &workspacev1.DeleteBaseRequest{
			BaseId:  baseID,
			Confirm: true,
		}); err != nil {
			t.Fatalf("DeleteBase() attempt %d error = %v", attempt+1, err)
		}
	}
}

func TestDeleteBaseWithoutTablesSucceeds(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "无数据表",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	baseID := created.GetBase().GetId()
	if err := structure.DeleteAllForBase(
		context.Background(),
		harness.structure,
		baseID,
	); err != nil {
		t.Fatalf("DeleteAllForBase() error = %v", err)
	}
	if _, err := harness.client.DeleteBase(ctx, &workspacev1.DeleteBaseRequest{
		BaseId:  baseID,
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteBase() without tables error = %v", err)
	}
	if _, err := harness.repository.BaseByID(context.Background(), baseID); !errors.Is(err, base.ErrWorkspaceNotFound) {
		t.Fatalf("BaseByID(after delete) error = %v, want not found", err)
	}
}

func TestDeleteBaseRejectsNonOwnerMember(t *testing.T) {
	harness := newWorkspaceHarness(t)
	created, err := harness.client.CreateBase(
		authenticatedContext(t, testOwner, testEnterprise),
		&workspacev1.CreateBaseRequest{Name: "仅所有者可删"},
	)
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	_, err = harness.client.DeleteBase(
		authenticatedContext(t, testMember, testEnterprise),
		&workspacev1.DeleteBaseRequest{
			BaseId:  created.GetBase().GetId(),
			Confirm: true,
		},
	)
	requireErrorCode(t, err, 3001)
	if _, err := harness.repository.BaseByID(
		context.Background(),
		created.GetBase().GetId(),
	); err != nil {
		t.Fatalf("BaseByID() after rejected delete error = %v", err)
	}
}

func seedDeleteBaseCascade(
	t *testing.T,
	db *sql.DB,
	baseID, tableID string,
) {
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
			args: []any{deleteBaseRecordID, tableID, testOwner, now},
		},
		{
			query: `INSERT INTO excel_records
				(id, table_id, cells_json, created_by, created_at, version, soft_deleted_at)
				VALUES (?, ?, JSON_OBJECT(), ?, ?, 2, ?)`,
			args: []any{deleteBaseDeletedRecID, tableID, testOwner, now, now},
		},
		{
			query: "INSERT INTO excel_records_histories\n" +
				"(id, record_id, field_id, `before`, `after`, operator, created_at)\n" +
				"VALUES (?, ?, ?, JSON_OBJECT(), JSON_OBJECT(), ?, ?)",
			args: []any{
				deleteBaseHistoryID, deleteBaseRecordID, deleteBaseFieldID,
				testOwner, now,
			},
		},
		{
			query: `INSERT INTO excel_comment_comments
				(id, record_id, parent_id, content, author, status, mentions, created_at)
				VALUES (?, ?, NULL, '待删评论', ?, 'OPEN', JSON_ARRAY(), ?)`,
			args: []any{deleteBaseCommentID, deleteBaseRecordID, testOwner, now},
		},
		{
			query: `INSERT INTO excel_comment_comments
				(id, record_id, parent_id, content, author, status, mentions, created_at)
				VALUES (?, ?, ?, '待删回复', ?, 'OPEN', JSON_ARRAY(), ?)`,
			args: []any{
				deleteBaseReplyID, deleteBaseRecordID, deleteBaseCommentID,
				testOwner, now,
			},
		},
		{
			query: `INSERT INTO excel_share_links
				(base_id, token, scope, role, enabled, url, updated_at)
				VALUES (?, ?, 'anyone', 'viewer', 1, '', ?)`,
			args: []any{baseID, deleteBaseLinkToken, now},
		},
		{
			query: `INSERT INTO excel_calc_materialized
				(record_id, field_id, value_json, formula_hash, status, reason)
				VALUES (?, ?, NULL, '', 'IDLE', '')`,
			args: []any{deleteBaseRecordID, deleteBaseFieldID},
		},
		{
			query: `INSERT INTO excel_calc_jobs
				(job_id, table_id, status, payload, attempts)
				VALUES (?, ?, 'CALC', JSON_OBJECT(), 1)`,
			args: []any{deleteBaseJobID, tableID},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES (?, 'create_record', ?, JSON_OBJECT(), ?)`,
			args: []any{
				deleteBaseOplogID, `{"table_id":"` + tableID + `"}`, now,
			},
		},
		{
			query: `INSERT INTO excel_records_oplog
				(client_request_id, kind, request_json, response_json, created_at)
				VALUES (?, 'update_cell', ?, JSON_OBJECT(), ?)`,
			args: []any{
				deleteBaseRecordOplogID,
				`{"record_id":"` + deleteBaseRecordID + `"}`,
				now,
			},
		},
	} {
		if _, err := db.ExecContext(
			context.Background(),
			statement.query,
			statement.args...,
		); err != nil {
			t.Fatalf("seed delete Base rows with %q: %v", statement.query, err)
		}
	}
}

func assertDeleteBaseRowsGone(
	t *testing.T,
	db *sql.DB,
	baseID, tableID string,
) {
	t.Helper()
	for _, check := range []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "records",
			query: `SELECT COUNT(*) FROM excel_records WHERE table_id = ?`,
			args:  []any{tableID},
		},
		{
			name:  "histories",
			query: `SELECT COUNT(*) FROM excel_records_histories WHERE record_id = ?`,
			args:  []any{deleteBaseRecordID},
		},
		{
			name: "comments",
			query: `SELECT COUNT(*) FROM excel_comment_comments
				WHERE record_id = ?`,
			args: []any{deleteBaseRecordID},
		},
		{
			name:  "share links",
			query: `SELECT COUNT(*) FROM excel_share_links WHERE base_id = ?`,
			args:  []any{baseID},
		},
		{
			name:  "calc materialized",
			query: `SELECT COUNT(*) FROM excel_calc_materialized WHERE record_id = ?`,
			args:  []any{deleteBaseRecordID},
		},
		{
			name:  "calc jobs",
			query: `SELECT COUNT(*) FROM excel_calc_jobs WHERE table_id = ?`,
			args:  []any{tableID},
		},
		{
			name: "oplog",
			query: `SELECT COUNT(*) FROM excel_records_oplog
				WHERE client_request_id IN (?, ?)`,
			args: []any{deleteBaseOplogID, deleteBaseRecordOplogID},
		},
		{
			name:  "structure tables",
			query: `SELECT COUNT(*) FROM excel_structure_tables WHERE base_id = ?`,
			args:  []any{baseID},
		},
	} {
		var count int
		if err := db.QueryRowContext(
			context.Background(),
			check.query,
			check.args...,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after delete = %d, want 0", check.name, count)
		}
	}
}

func assertDeleteBaseAudit(t *testing.T, harness *workspaceHarness) {
	t.Helper()
	if len(harness.outbox.events) != 1 {
		t.Fatalf("outbox events = %d, want 1", len(harness.outbox.events))
	}
	forwarded, err := harness.forwarder.ForwardOnce(context.Background())
	if err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}
	if forwarded != 1 {
		t.Fatalf("ForwardOnce() forwarded = %d, want 1", forwarded)
	}
	deliveryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	delivery, err := harness.auditEvents.Receive(deliveryCtx)
	if err != nil {
		t.Fatalf("Receive(audit.append) error = %v", err)
	}
	event := delivery.Event
	if event.Header.Type != "audit.append" ||
		event.Header.Producer != "workspace" ||
		event.Header.BaseID == "" {
		t.Fatalf("audit event header = %+v", event.Header)
	}
	var payload struct {
		Action    string         `json:"action"`
		Operator  string         `json:"operator"`
		Target    string         `json:"target"`
		DetailMap map[string]any `json:"detail_map"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if payload.Action != "AUDIT_ACTION_BASE_DELETED" ||
		payload.Operator != testOwner ||
		payload.Target != event.Header.BaseID ||
		payload.DetailMap["enterprise_id"] != testEnterprise {
		t.Fatalf("audit payload = %+v", payload)
	}
}
