// records/collaborator_write_test.go：外部协作者（principal enterprise_id 为空）
// 在 Base 上的读写契约。锁定 P0 冒烟「添加协作者后立即可写」验收：
// 网关 CheckAccess（share）已按 Base 粒度裁决外部协作者，records 各 seam
// 不得再以协作者自身（空）企业过滤结构表——租户标识取表行自身的
// enterprise_id（即 Base 所属企业）。接线与 cmd/main.go 生产装配一致
// （真实 StructureRepository / ShareBaseResolver / RowScopeProvider）。
package records_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"excel-libs/testmysql"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/iam"
	"excel_core_service/internal/records"
	"excel_core_service/internal/share"
	"excel_core_service/internal/workspace"
)

const (
	collaboratorTestEnterpriseID = "01J00000000000000000009001"
	collaboratorTestBaseID       = "01J00000000000000000009002"
	collaboratorTestTableID      = "01J00000000000000000009003"
	collaboratorTestFieldID      = "01J00000000000000000009004"
	collaboratorTestOwnerID      = "01J00000000000000000009005"
	collaboratorTestOwnerRowID   = "01J00000000000000000009006"
	collaboratorTestFillerID     = "01J00000000000000000009007"
	collaboratorTestEditorID     = "01J00000000000000000009008"
	collaboratorTestViewerID     = "01J00000000000000000009009"
)

type collaboratorHarness struct {
	share   sharev1.ShareServiceClient
	records recordsv1.RecordsServiceClient
	db      *sql.DB
	now     time.Time
}

// newCollaboratorHarness 按 cmd/main.go 的生产装配接线：share 与 records 共用
// 一个 gRPC server，records 的结构访问与 Base 上下文全部走真实
// StructureRepository，行级范围走真实 share.ResolveRole 适配。
func newCollaboratorHarness(t *testing.T) *collaboratorHarness {
	t.Helper()
	db := testmysql.OpenTestDB(t, recordsTestMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUpWithRetry(db); err != nil {
		t.Fatalf("base.MigrateUpWithRetry() error = %v", err)
	}
	resetCollaboratorData(t, db)
	t.Cleanup(func() { resetCollaboratorData(t, db) })

	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	seedCollaboratorFixtures(t, db, now)

	structureRepository := base.NewStructureRepository(db)
	workspaceRepository := base.NewWorkspaceRepository(db)
	shareService, err := share.NewService(share.ServiceDeps{
		Repository:           base.NewShareRepository(db),
		Outbox:               bus.NewOutbox(base.NewBusStore(db)),
		Bases:                workspace.NewShareBaseResolver(workspaceRepository),
		BaseOwner:            workspace.NewShareBaseOwnerProvider(workspaceRepository),
		ImportLock:           workspace.NewShareBaseResolver(workspaceRepository),
		Members:              iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
		EnterpriseDirectory:  iam.NewEnterpriseDirectoryProvider(base.NewIAMRepository(db)),
		PermissionConfigPath: writeCollaboratorPermissionPolicy(t),
	})
	if err != nil {
		t.Fatalf("share.NewService() error = %v", err)
	}
	recordsService := records.NewService(
		base.NewRecordsRepository(db),
		records.NewStructureViewAccessor(structureRepository, records.NewShareBaseAccess(shareService)),
		records.NewShareRowScopeProvider(shareService),
		records.NewShareRoleAccess(shareService),
		nil,
		func() time.Time { return now },
		base.NewCommentRepository(db),
		records.WriterOptions{
			Outbox: bus.NewOutbox(base.NewBusStore(db)),
			Bases: records.NewStructureBaseContextProvider(
				structureRepository,
				records.NewShareBaseAccess(shareService),
			),
			ComputedWriter: records.NewCalcComputedWriter(base.NewCalcRepository(db)),
		},
	)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	share.Register(server, shareService)
	records.Register(server, recordsService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///collaborator-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &collaboratorHarness{
		share:   sharev1.NewShareServiceClient(connection),
		records: recordsv1.NewRecordsServiceClient(connection),
		db:      db,
		now:     now,
	}
}

func resetCollaboratorData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_oplog",
		"DELETE FROM excel_records_histories",
		"DELETE FROM excel_records",
		"DELETE FROM excel_comment_comments",
		"DELETE FROM excel_calc_jobs",
		"DELETE FROM excel_calc_materialized",
		"DELETE FROM excel_bus_outbox",
		"DELETE FROM excel_structure_field_options",
		"DELETE FROM excel_structure_views",
		"DELETE FROM excel_structure_fields",
		"DELETE FROM excel_structure_tables",
		"DELETE FROM excel_share_collaborators",
		"DELETE FROM excel_share_links",
		"DELETE FROM excel_workspace_stars",
		"DELETE FROM excel_workspace_recent",
		"DELETE FROM excel_workspace_bases",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset collaborator data with %q: %v", statement, err)
		}
	}
}

func seedCollaboratorFixtures(t *testing.T, db *sql.DB, now time.Time) {
	t.Helper()
	ctx := context.Background()
	timestamp := now.UnixMilli()
	fixtures := []struct {
		query string
		args  []any
	}{
		{
			"INSERT INTO excel_workspace_bases " +
				"(id, enterprise_id, owner_id, name, last_modified_at, last_modifier, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{
				collaboratorTestBaseID,
				collaboratorTestEnterpriseID,
				collaboratorTestOwnerID,
				"协作者冒烟",
				timestamp,
				collaboratorTestOwnerID,
				timestamp,
			},
		},
		{
			"INSERT INTO excel_structure_tables " +
				"(id, enterprise_id, base_id, name, position, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{
				collaboratorTestTableID,
				collaboratorTestEnterpriseID,
				collaboratorTestBaseID,
				"填报表",
				0,
				timestamp,
			},
		},
		{
			"INSERT INTO excel_structure_fields " +
				"(id, table_id, name, type, config, is_primary, position, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{
				collaboratorTestFieldID,
				collaboratorTestTableID,
				"标题",
				"FIELD_TYPE_TEXT",
				"{}",
				1,
				0,
				timestamp,
			},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'filler', 0, 'invite', ?)",
			[]any{collaboratorTestBaseID, collaboratorTestFillerID, timestamp},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'editor', 0, 'invite', ?)",
			[]any{collaboratorTestBaseID, collaboratorTestEditorID, timestamp},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'viewer', 0, 'invite', ?)",
			[]any{collaboratorTestBaseID, collaboratorTestViewerID, timestamp},
		},
	}
	for _, fixture := range fixtures {
		if _, err := db.ExecContext(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatalf("seed collaborator fixture %q: %v", fixture.query, err)
		}
	}
	// 所有者已有一行：filler 的 OWN 视野应看不到它，editor/viewer 应看得到。
	repository := base.NewRecordsRepository(db)
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		t.Fatalf("seed owner record begin tx: %v", err)
	}
	if err := repository.CreateRecordTx(ctx, tx, base.Record{
		ID:      collaboratorTestOwnerRowID,
		TableID: collaboratorTestTableID,
		Cells: map[string]*commonv1.Value{
			collaboratorTestFieldID: collaboratorStringValue("所有者的行"),
		},
		CreatedBy: collaboratorTestOwnerID,
		CreatedAt: timestamp,
		Version:   1,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed owner record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("seed owner record commit: %v", err)
	}
}

func writeCollaboratorPermissionPolicy(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "permissions.json")
	payload := `{
		"schema_version": 1,
		"roles": {
			"manager": {"name": "可管理", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "manage_structure", "comment", "export", "manage_share", "copy_base", "import"]},
			"editor": {"name": "可编辑", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "comment", "export"]},
			"filler": {"name": "可填写", "actions": ["view_own", "create_record", "edit_own", "comment"]},
			"viewer": {"name": "可查看", "actions": ["view", "comment"]}
		},
		"external": {"export_allowed": false}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write permission policy: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("set permission policy mtime: %v", err)
	}
	return path
}

// collaboratorContext mirrors the gateway call shape: CheckAccess decision is
// forwarded as x-row-scope/x-base-id metadata next to x-principal.
func collaboratorContext(
	t *testing.T,
	userID, enterpriseID string,
	internal bool,
	rowScope sharev1.RowScope,
	baseID string,
) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     internal,
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	ctx := metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
	if rowScope != sharev1.RowScope_ROW_SCOPE_UNSPECIFIED {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-row-scope", rowScope.String())
	}
	if baseID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-base-id", baseID)
	}
	return ctx
}

func externalCollaboratorContext(
	t *testing.T,
	userID string,
	rowScope sharev1.RowScope,
) context.Context {
	return collaboratorContext(t, userID, "", false, rowScope, collaboratorTestBaseID)
}

func collaboratorStringValue(value string) *commonv1.Value {
	return &commonv1.Value{
		Kind: &commonv1.Value_StringValue{StringValue: value},
	}
}

// collaboratorBusinessCode extracts the ErrorInfo reason business code; 0 means
// the error carries no contract business code (e.g. a raw gRPC Unknown).
func collaboratorBusinessCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want business code")
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	for _, detail := range grpcStatus.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			if code, convErr := strconv.Atoi(info.GetReason()); convErr == nil {
				return code
			}
		}
	}
	return 0
}

// TestExternalCollaboratorFillerGrantedThenImmediatelyWrites 锁票 03「添加协
// 作者后立即可写」验收：外部 filler 授权后 CheckAccess 立即 allow（行级 OWN），
// 创建/读取/改自己的行全部成功——修复前写路径 9001（ContextByTable 以空企业
// 查表，裸包错误映射缺省）、读路径 4002（tableForBase 同因误报已删除）。
func TestExternalCollaboratorFillerGrantedThenImmediatelyWrites(t *testing.T) {
	harness := newCollaboratorHarness(t)
	ctx := context.Background()

	decision, err := harness.share.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{UserId: collaboratorTestFillerID},
		BaseId:    collaboratorTestBaseID,
		Action:    sharev1.Action_ACTION_CREATE_RECORD,
	})
	if err != nil {
		t.Fatalf("CheckAccess(create_record) error = %v", err)
	}
	if !decision.GetAllow() || decision.GetRowScope() != sharev1.RowScope_ROW_SCOPE_OWN {
		t.Fatalf(
			"CheckAccess(create_record) = allow %v row_scope %v, want allow/OWN",
			decision.GetAllow(),
			decision.GetRowScope(),
		)
	}

	createCtx := externalCollaboratorContext(
		t,
		collaboratorTestFillerID,
		decision.GetRowScope(),
	)
	created, err := harness.records.CreateRecord(createCtx, &recordsv1.CreateRecordRequest{
		TableId: collaboratorTestTableID,
		CellsMap: map[string]*commonv1.Value{
			collaboratorTestFieldID: collaboratorStringValue("外部填报"),
		},
	})
	if err != nil {
		t.Fatalf("CreateRecord(external filler) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if created.GetRecord().GetCreatedBy() != collaboratorTestFillerID {
		t.Fatalf(
			"CreateRecord created_by = %q, want collaborator %q",
			created.GetRecord().GetCreatedBy(),
			collaboratorTestFillerID,
		)
	}
	if created.GetRecord().GetVersion() != 1 {
		t.Fatalf("CreateRecord version = %d, want 1", created.GetRecord().GetVersion())
	}

	// 读路径：只看得见自己的行（所有者既有的行被 OWN 范围过滤），绝不 4002。
	listed, err := harness.records.QueryView(createCtx, &recordsv1.QueryViewRequest{
		BaseId:  collaboratorTestBaseID,
		TableId: collaboratorTestTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(external filler) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	rows := listed.GetRecordList()
	if len(rows) != 1 || rows[0].GetId() != created.GetRecord().GetId() {
		t.Fatalf("QueryView(external filler) = %d rows, want exactly the own row %q", len(rows), created.GetRecord().GetId())
	}

	// 自己的行可改，乐观锁版本 bump 到 2。
	updated, err := harness.records.UpdateCell(createCtx, &recordsv1.UpdateCellRequest{
		RecordId:      created.GetRecord().GetId(),
		FieldId:       collaboratorTestFieldID,
		Value:         collaboratorStringValue("外部填报-改"),
		ExpectVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateCell(external filler own row) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if updated.GetVersion() != 2 {
		t.Fatalf("UpdateCell version = %d, want 2", updated.GetVersion())
	}
}

// TestExternalCollaboratorEditorWritesAndSeesAllRows 锁外部 editor 契约：
// 行级 ALL——创建成功且能改所有者的行；读取能看到全部行（非 4002）。
func TestExternalCollaboratorEditorWritesAndSeesAllRows(t *testing.T) {
	harness := newCollaboratorHarness(t)
	ctx := context.Background()

	decision, err := harness.share.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{UserId: collaboratorTestEditorID},
		BaseId:    collaboratorTestBaseID,
		Action:    sharev1.Action_ACTION_EDIT_ALL,
	})
	if err != nil {
		t.Fatalf("CheckAccess(edit_all) error = %v", err)
	}
	if !decision.GetAllow() || decision.GetRowScope() != sharev1.RowScope_ROW_SCOPE_ALL {
		t.Fatalf(
			"CheckAccess(edit_all) = allow %v row_scope %v, want allow/ALL",
			decision.GetAllow(),
			decision.GetRowScope(),
		)
	}

	editorCtx := externalCollaboratorContext(
		t,
		collaboratorTestEditorID,
		decision.GetRowScope(),
	)
	created, err := harness.records.CreateRecord(editorCtx, &recordsv1.CreateRecordRequest{
		TableId: collaboratorTestTableID,
		CellsMap: map[string]*commonv1.Value{
			collaboratorTestFieldID: collaboratorStringValue("外部编辑"),
		},
	})
	if err != nil {
		t.Fatalf("CreateRecord(external editor) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}

	listed, err := harness.records.QueryView(editorCtx, &recordsv1.QueryViewRequest{
		BaseId:  collaboratorTestBaseID,
		TableId: collaboratorTestTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(external editor) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	rows := listed.GetRecordList()
	if len(rows) != 2 {
		t.Fatalf("QueryView(external editor) = %d rows, want 2 (own + owner's)", len(rows))
	}
	sawCreated := false
	for _, row := range rows {
		if row.GetId() == created.GetRecord().GetId() {
			sawCreated = true
		}
	}
	if !sawCreated {
		t.Fatalf("QueryView(external editor) missing own row %q", created.GetRecord().GetId())
	}

	// editor 行级 ALL：可改所有者的行。
	updated, err := harness.records.UpdateCell(editorCtx, &recordsv1.UpdateCellRequest{
		RecordId:      collaboratorTestOwnerRowID,
		FieldId:       collaboratorTestFieldID,
		Value:         collaboratorStringValue("外部编辑-改所有者"),
		ExpectVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateCell(external editor on owner row) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if updated.GetVersion() != 2 {
		t.Fatalf("UpdateCell(owner row) version = %d, want 2", updated.GetVersion())
	}
}

// TestExternalCollaboratorViewerReadsAllRowsButWritesDenied 锁外部 viewer 契约：
// CheckAccess 对写动作 deny（网关据此回 3001）；行级范围为 ALL，读路径返回
// 全部行而非 4002 误报。
func TestExternalCollaboratorViewerReadsAllRowsButWritesDenied(t *testing.T) {
	harness := newCollaboratorHarness(t)
	ctx := context.Background()

	denied, err := harness.share.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{UserId: collaboratorTestViewerID},
		BaseId:    collaboratorTestBaseID,
		Action:    sharev1.Action_ACTION_CREATE_RECORD,
	})
	if err != nil {
		t.Fatalf("CheckAccess(create_record, viewer) error = %v", err)
	}
	if denied.GetAllow() {
		t.Fatal("CheckAccess(create_record, viewer) allow = true, want deny (gateway 3001)")
	}
	if denied.GetRowScope() != sharev1.RowScope_ROW_SCOPE_ALL {
		t.Fatalf("CheckAccess(create_record, viewer) row_scope = %v, want ALL", denied.GetRowScope())
	}

	viewerCtx := externalCollaboratorContext(
		t,
		collaboratorTestViewerID,
		denied.GetRowScope(),
	)
	listed, err := harness.records.QueryView(viewerCtx, &recordsv1.QueryViewRequest{
		BaseId:  collaboratorTestBaseID,
		TableId: collaboratorTestTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(external viewer) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if len(listed.GetRecordList()) != 1 ||
		listed.GetRecordList()[0].GetId() != collaboratorTestOwnerRowID {
		t.Fatalf(
			"QueryView(external viewer) = %d rows, want the owner row %q",
			len(listed.GetRecordList()),
			collaboratorTestOwnerRowID,
		)
	}
}

// TestInternalMemberRecordsPathUnchanged 锁内部成员（principal 带所属企业）
// 既有路径：真实 ContextByTable/结构访问仍按企业租户断言命中，创建、读取、
// 改行全部成功。
func TestInternalMemberRecordsPathUnchanged(t *testing.T) {
	harness := newCollaboratorHarness(t)

	memberCtx := collaboratorContext(
		t,
		collaboratorTestOwnerID,
		collaboratorTestEnterpriseID,
		true,
		sharev1.RowScope_ROW_SCOPE_ALL,
		collaboratorTestBaseID,
	)
	created, err := harness.records.CreateRecord(memberCtx, &recordsv1.CreateRecordRequest{
		TableId: collaboratorTestTableID,
		CellsMap: map[string]*commonv1.Value{
			collaboratorTestFieldID: collaboratorStringValue("内部成员"),
		},
	})
	if err != nil {
		t.Fatalf("CreateRecord(internal member) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if created.GetRecord().GetCreatedBy() != collaboratorTestOwnerID {
		t.Fatalf(
			"CreateRecord created_by = %q, want member %q",
			created.GetRecord().GetCreatedBy(),
			collaboratorTestOwnerID,
		)
	}

	listed, err := harness.records.QueryView(memberCtx, &recordsv1.QueryViewRequest{
		BaseId:  collaboratorTestBaseID,
		TableId: collaboratorTestTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(internal member) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if len(listed.GetRecordList()) != 2 {
		t.Fatalf("QueryView(internal member) = %d rows, want 2", len(listed.GetRecordList()))
	}

	updated, err := harness.records.UpdateCell(memberCtx, &recordsv1.UpdateCellRequest{
		RecordId:      created.GetRecord().GetId(),
		FieldId:       collaboratorTestFieldID,
		Value:         collaboratorStringValue("内部成员-改"),
		ExpectVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateCell(internal member) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if updated.GetVersion() != 2 {
		t.Fatalf("UpdateCell version = %d, want 2", updated.GetVersion())
	}
}
