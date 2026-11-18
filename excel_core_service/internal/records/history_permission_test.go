// records/history_permission_test.go：记录历史可见性契约（工单 10，缺陷
// fullflow #19 TC-080-09）。锁定 spec §4 决策：viewer 拉取 H-55 返回 3001；
// filler 仅自己可见（自己创建）记录的历史；editor/manager 全量；越权统一
// 3001（ADR 0003：先存在/可操作性，后归属）。夹具 ID 一律以 01JT10 前缀命名
// 并按前缀清理，不触碰其它夹具数据。
package records_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
	"excel_core_service/internal/iam"
	"excel_core_service/internal/records"
	"excel_core_service/internal/share"
	"excel_core_service/internal/workspace"
)

const (
	// t10HistoryEnterpriseID 是 Base 所属企业（数据租户）。
	t10HistoryEnterpriseID = "01JT10ENTC0000000000000000"
	// t10HistoryBaseID / t10HistoryTableID 是历史可见性矩阵的 Base 与数据表。
	t10HistoryBaseID  = "01JT10BASE2000000000000000"
	t10HistoryTableID = "01JT10TAB30000000000000000"
	// t10HistOwnerRecordID 由 Base 所有者创建；t10HistFillerRecordID 由 filler 创建。
	t10HistOwnerRecordID  = "01JT10REC30000000000000000"
	t10HistFillerRecordID = "01JT10REC40000000000000000"
	// t10OwnerMemberID 是内部企业成员兼 Base 所有者（可管理对照组）。
	t10OwnerMemberID = "01JT10OWN20000000000000000"
	// t10FillerMemberID / t10ViewerMemberID / t10EditorMemberID 是外部协作者条目。
	t10FillerMemberID = "01JT10FIL20000000000000000"
	t10ViewerMemberID = "01JT10VIE20000000000000000"
	t10EditorMemberID = "01JT10EDT20000000000000000"
	// t10StrangerMemberID 无任何 Base 授权。
	t10StrangerMemberID = "01JT10STR20000000000000000"
)

type historyPermissionHarness struct {
	client         recordsv1.RecordsServiceClient
	db             *sql.DB
	ownerRecordID  string
	fillerRecordID string
	ownerUserID    string
	fillerUserID   string
	viewerUserID   string
	editorUserID   string
	strangerUserID string
}

// TestGetHistoryVisibilityFollowsBaseRole 锁定历史可见性矩阵（spec §4）。
func TestGetHistoryVisibilityFollowsBaseRole(t *testing.T) {
	harness := newHistoryPermissionHarness(t)

	// filler 拉取自己创建记录的历史 → 成功。
	fillerOwn, err := harness.client.GetHistory(
		t10HistoryContext(t, harness.fillerUserID, false, sharev1.RowScope_ROW_SCOPE_OWN),
		&recordsv1.GetHistoryRequest{RecordId: harness.fillerRecordID, Limit: 100},
	)
	if err != nil {
		t.Fatalf("GetHistory(filler own record) error = %v", err)
	}
	if len(fillerOwn.GetHistoryList()) != 1 ||
		fillerOwn.GetHistoryList()[0].GetId() != "hst_t10_filler" {
		t.Fatalf("filler own history = %+v", fillerOwn.GetHistoryList())
	}

	// filler 拉取他人记录历史 → 3001。
	_, err = harness.client.GetHistory(
		t10HistoryContext(t, harness.fillerUserID, false, sharev1.RowScope_ROW_SCOPE_OWN),
		&recordsv1.GetHistoryRequest{RecordId: harness.ownerRecordID},
	)
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)

	// viewer 拉取历史 → 3001（无论记录归属，网关缺省 ROW_SCOPE_ALL 也不放行）。
	_, err = harness.client.GetHistory(
		t10HistoryContext(t, harness.viewerUserID, false, sharev1.RowScope_ROW_SCOPE_ALL),
		&recordsv1.GetHistoryRequest{RecordId: harness.ownerRecordID},
	)
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)
	_, err = harness.client.GetHistory(
		t10HistoryContext(t, harness.viewerUserID, false, sharev1.RowScope_ROW_SCOPE_ALL),
		&recordsv1.GetHistoryRequest{RecordId: harness.fillerRecordID},
	)
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)

	// 无授权协作者拉取历史 → 3001。
	_, err = harness.client.GetHistory(
		t10HistoryContext(t, harness.strangerUserID, false, sharev1.RowScope_ROW_SCOPE_ALL),
		&recordsv1.GetHistoryRequest{RecordId: harness.ownerRecordID},
	)
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)

	// editor 拉取他人记录历史 → 成功（矩阵放行）。
	editorView, err := harness.client.GetHistory(
		t10HistoryContext(t, harness.editorUserID, false, sharev1.RowScope_ROW_SCOPE_ALL),
		&recordsv1.GetHistoryRequest{RecordId: harness.ownerRecordID, Limit: 100},
	)
	if err != nil {
		t.Fatalf("GetHistory(editor) error = %v", err)
	}
	if len(editorView.GetHistoryList()) != 1 ||
		editorView.GetHistoryList()[0].GetId() != "hst_t10_owner" {
		t.Fatalf("editor history = %+v", editorView.GetHistoryList())
	}

	// 可管理者（内部成员兼 Base 所有者）拉取 filler 记录历史 → 成功。
	managerView, err := harness.client.GetHistory(
		t10HistoryContext(t, harness.ownerUserID, true, sharev1.RowScope_ROW_SCOPE_ALL),
		&recordsv1.GetHistoryRequest{RecordId: harness.fillerRecordID, Limit: 100},
	)
	if err != nil {
		t.Fatalf("GetHistory(manager) error = %v", err)
	}
	if len(managerView.GetHistoryList()) != 1 ||
		managerView.GetHistoryList()[0].GetId() != "hst_t10_filler" {
		t.Fatalf("manager history = %+v", managerView.GetHistoryList())
	}
}

func newHistoryPermissionHarness(t *testing.T) *historyPermissionHarness {
	t.Helper()
	db := testmysql.OpenTestDB(t, recordsTestMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetHistoryPermissionData(t, db)
	t.Cleanup(func() { resetHistoryPermissionData(t, db) })

	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	seedHistoryPermissionFixtures(t, db, now)

	structureRepository := base.NewStructureRepository(db)
	workspaceRepository := base.NewWorkspaceRepository(db)
	shareService, err := share.NewService(share.ServiceDeps{
		Repository:           base.NewShareRepository(db),
		Outbox:               bus.NewOutbox(base.NewBusStore(db)),
		Bases:                workspace.NewShareBaseResolver(workspaceRepository),
		ImportLock:           workspace.NewShareBaseResolver(workspaceRepository),
		BaseOwner:            workspace.NewShareBaseOwnerProvider(workspaceRepository),
		Members:              iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
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
	)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	records.Register(server, recordsService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///history-permission-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &historyPermissionHarness{
		client:         recordsv1.NewRecordsServiceClient(connection),
		db:             db,
		ownerRecordID:  t10HistOwnerRecordID,
		fillerRecordID: t10HistFillerRecordID,
		ownerUserID:    t10OwnerMemberID,
		fillerUserID:   t10FillerMemberID,
		viewerUserID:   t10ViewerMemberID,
		editorUserID:   t10EditorMemberID,
		strangerUserID: t10StrangerMemberID,
	}
}

// resetHistoryPermissionData 只清理 01JT10 前缀夹具，不影响并行测试数据。
func resetHistoryPermissionData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_histories WHERE record_id LIKE '01JT10%'",
		"DELETE FROM excel_records WHERE id LIKE '01JT10%' OR table_id LIKE '01JT10%'",
		"DELETE FROM excel_comment_comments WHERE record_id LIKE '01JT10%'",
		"DELETE FROM excel_bus_outbox WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_structure_tables WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_share_collaborators WHERE base_id LIKE '01JT10%' OR user_id LIKE '01JT10%'",
		"DELETE FROM excel_share_links WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_workspace_stars WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_workspace_recent WHERE base_id LIKE '01JT10%'",
		"DELETE FROM excel_workspace_bases WHERE id LIKE '01JT10%'",
		"DELETE FROM excel_iam_members WHERE enterprise_id LIKE '01JT10%' OR user_id LIKE '01JT10%'",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset history permission data with %q: %v", statement, err)
		}
	}
}

func seedHistoryPermissionFixtures(t *testing.T, db *sql.DB, now time.Time) {
	t.Helper()
	timestamp := now.UnixMilli()
	fixtures := []struct {
		query string
		args  []any
	}{
		{
			"INSERT INTO excel_workspace_bases " +
				"(id, enterprise_id, owner_id, name, last_modified_at, last_modifier, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{t10HistoryBaseID, t10HistoryEnterpriseID, t10OwnerMemberID, "历史可见性 Base", timestamp, t10OwnerMemberID, timestamp},
		},
		{
			"INSERT INTO excel_iam_members " +
				"(enterprise_id, user_id, role, status, joined_at) VALUES (?, ?, 'owner', 'active', ?)",
			[]any{t10HistoryEnterpriseID, t10OwnerMemberID, timestamp},
		},
		{
			"INSERT INTO excel_structure_tables " +
				"(id, enterprise_id, base_id, name, position, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{t10HistoryTableID, t10HistoryEnterpriseID, t10HistoryBaseID, "历史表", 0, timestamp},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'filler', 0, 'invite', ?)",
			[]any{t10HistoryBaseID, t10FillerMemberID, timestamp},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'viewer', 0, 'invite', ?)",
			[]any{t10HistoryBaseID, t10ViewerMemberID, timestamp},
		},
		{
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'editor', 0, 'invite', ?)",
			[]any{t10HistoryBaseID, t10EditorMemberID, timestamp},
		},
		{
			"INSERT INTO excel_records " +
				"(id, table_id, cells_json, created_by, created_at, version) " +
				"VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)",
			[]any{t10HistOwnerRecordID, t10HistoryTableID, t10OwnerMemberID, timestamp},
		},
		{
			"INSERT INTO excel_records " +
				"(id, table_id, cells_json, created_by, created_at, version) " +
				"VALUES (?, ?, JSON_OBJECT(), ?, ?, 1)",
			[]any{t10HistFillerRecordID, t10HistoryTableID, t10FillerMemberID, timestamp},
		},
	}
	for _, fixture := range fixtures {
		if _, err := db.ExecContext(context.Background(), fixture.query, fixture.args...); err != nil {
			t.Fatalf("seed history permission fixture %q: %v", fixture.query, err)
		}
	}
	seedHistory(
		t,
		db,
		"hst_t10_owner",
		t10HistOwnerRecordID,
		"fld_t10",
		nil,
		stringCell("owner 值"),
		t10OwnerMemberID,
		now.Add(-time.Hour).UnixMilli(),
	)
	seedHistory(
		t,
		db,
		"hst_t10_filler",
		t10HistFillerRecordID,
		"fld_t10",
		nil,
		stringCell("filler 值"),
		t10FillerMemberID,
		now.Add(-2*time.Hour).UnixMilli(),
	)
}

// t10HistoryContext mirrors the gateway call shape: principal plus the
// x-row-scope metadata derived from the collaborator's effective role. 内部成员
// 携带企业归属（与 Base 企业一致），外部协作者无企业归属。
func t10HistoryContext(
	t *testing.T,
	userID string,
	internal bool,
	rowScope sharev1.RowScope,
) context.Context {
	t.Helper()
	principal := &commonv1.Principal{UserId: userID}
	if internal {
		principal.EnterpriseId = t10HistoryEnterpriseID
		principal.Internal = true
	}
	payload, err := proto.Marshal(principal)
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
		"x-row-scope",
		rowScope.String(),
	)
}
