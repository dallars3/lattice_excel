// records/external_collaborator_tenant_test.go：外部协作者租户边界契约（票 08，
// 缺陷 fullflow #13）。锁定「租户按数据表所属 Base 的企业解析，而不是按
// Principal 自身 enterprise_id」（spec §4，ADR 0004）：显式外部协作者即使
// 带其它企业归属（对测企业成员），在 Base 授权内 H-48/H-49/H-51 全部可用，
// 陈旧版本 4001，越权 fail-closed 3001；链接会话与内部成员行为不回归。
// 夹具 ID 一律以 01JT08 前缀命名并按前缀清理，不触碰其它夹具数据。
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
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
	"excel_core_service/internal/iam"
	"excel_core_service/internal/records"
	"excel_core_service/internal/share"
	"excel_core_service/internal/workspace"
)

const (
	// t08ExternalTenantEnterpriseID 是 Base 所属企业（数据租户）。
	t08ExternalTenantEnterpriseID = "01JT08ENTA0000000000000000"
	// t08ForeignEnterpriseID 是外部协作者自带的「对测」企业（与 Base 不同企业）。
	t08ForeignEnterpriseID = "01JT08ENTB0000000000000000"
	// t08SharedBaseID / t08OtherBaseID 是两个互不授权的 Base。
	t08SharedBaseID = "01JT08BASE1000000000000000"
	t08OtherBaseID  = "01JT08BASE2000000000000000"
	// t08SharedTableID / t08SharedFieldID / t08OwnerRowID 属于 t08SharedBaseID。
	t08SharedTableID = "01JT08TAB10000000000000000"
	t08SharedFieldID = "01JT08FLD10000000000000000"
	t08OwnerRowID    = "01JT08REC10000000000000000"
	// t08OtherTableID / t08OtherFieldID / t08OtherRowID 属于 t08OtherBaseID。
	t08OtherTableID = "01JT08TAB20000000000000000"
	t08OtherFieldID = "01JT08FLD20000000000000000"
	t08OtherRowID   = "01JT08REC20000000000000000"
	// t08OwnerUserID 是 Base 所属企业的所有者（内部成员路径对照组）。
	t08OwnerUserID = "01JT08OWN10000000000000000"
	// t08ForeignEditorID 是带对测企业归属的显式外部可编辑协作者（缺陷 #13 peer）。
	t08ForeignEditorID = "01JT08FEDT0000000000000000"
	// t08ForeignStrangerID 带对测企业归属但无任何 Base 授权。
	t08ForeignStrangerID = "01JT08FSTR0000000000000000"
	// t08LinkUserID 是通过链接进入的访客会话用户（无企业归属）。
	t08LinkUserID = "01JT08LUSR0000000000000000"
	// t08FillerLinkToken / t08ViewerLinkToken 是两个有效分享链接。
	t08FillerLinkToken = "t08-filler-link-token"
	t08ViewerLinkToken = "t08-viewer-link-token"
)

type externalTenantHarness struct {
	share   sharev1.ShareServiceClient
	records recordsv1.RecordsServiceClient
	db      *sql.DB
	now     time.Time
}

// newExternalTenantHarness 按 cmd/main.go 生产装配接线（真实
// StructureRepository / ShareBaseResolver / RowScopeProvider / BaseReadAccess），
// 夹具只含 01JT08 前缀行，清理按前缀精确删除。
func newExternalTenantHarness(t *testing.T) *externalTenantHarness {
	t.Helper()
	db, err := base.OpenDB(recordsTestMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetExternalTenantData(t, db)
	t.Cleanup(func() { resetExternalTenantData(t, db) })

	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	seedExternalTenantFixtures(t, db, now)

	structureRepository := base.NewStructureRepository(db)
	workspaceRepository := base.NewWorkspaceRepository(db)
	shareService, err := share.NewService(share.ServiceDeps{
		Repository: base.NewShareRepository(db),
		Outbox:     bus.NewOutbox(base.NewBusStore(db)),
		Bases:      workspace.NewShareBaseResolver(workspaceRepository),
		BaseOwner:  workspace.NewShareBaseOwnerProvider(workspaceRepository),
		ImportLock: workspace.NewShareBaseResolver(workspaceRepository),
		Members:    iam.NewMemberRoleProvider(base.NewIAMRepository(db)),
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
		"passthrough:///external-tenant-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &externalTenantHarness{
		share:   sharev1.NewShareServiceClient(connection),
		records: recordsv1.NewRecordsServiceClient(connection),
		db:      db,
		now:     now,
	}
}

// resetExternalTenantData 只清理 01JT08 前缀夹具，不影响并行测试数据。
func resetExternalTenantData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_histories WHERE record_id LIKE '01JT08%'",
		"DELETE FROM excel_records WHERE id LIKE '01JT08%' OR table_id LIKE '01JT08%'",
		"DELETE FROM excel_comment_comments WHERE record_id LIKE '01JT08%'",
		"DELETE FROM excel_calc_materialized WHERE record_id LIKE '01JT08%'",
		"DELETE FROM excel_calc_jobs WHERE table_id LIKE '01JT08%'",
		"DELETE FROM excel_bus_outbox WHERE base_id LIKE '01JT08%'",
		"DELETE FROM excel_records_oplog WHERE client_request_id LIKE 't08-%'",
		"DELETE FROM excel_structure_field_options WHERE field_id LIKE '01JT08%'",
		"DELETE FROM excel_structure_views WHERE table_id LIKE '01JT08%'",
		"DELETE FROM excel_structure_fields WHERE table_id LIKE '01JT08%'",
		"DELETE FROM excel_structure_tables WHERE base_id LIKE '01JT08%'",
		"DELETE FROM excel_share_collaborators WHERE base_id LIKE '01JT08%' OR user_id LIKE '01JT08%'",
		"DELETE FROM excel_share_links WHERE base_id LIKE '01JT08%'",
		"DELETE FROM excel_workspace_stars WHERE base_id LIKE '01JT08%'",
		"DELETE FROM excel_workspace_recent WHERE base_id LIKE '01JT08%'",
		"DELETE FROM excel_workspace_bases WHERE id LIKE '01JT08%'",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset external tenant data with %q: %v", statement, err)
		}
	}
}

func seedExternalTenantFixtures(t *testing.T, db *sql.DB, now time.Time) {
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
			[]any{t08SharedBaseID, t08ExternalTenantEnterpriseID, t08OwnerUserID, "外部协作者租户-主 Base", timestamp, t08OwnerUserID, timestamp},
		},
		{
			"INSERT INTO excel_workspace_bases " +
				"(id, enterprise_id, owner_id, name, last_modified_at, last_modifier, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{t08OtherBaseID, t08ExternalTenantEnterpriseID, t08OwnerUserID, "外部协作者租户-其它 Base", timestamp, t08OwnerUserID, timestamp},
		},
		{
			"INSERT INTO excel_structure_tables " +
				"(id, enterprise_id, base_id, name, position, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{t08SharedTableID, t08ExternalTenantEnterpriseID, t08SharedBaseID, "协作表", 0, timestamp},
		},
		{
			"INSERT INTO excel_structure_tables " +
				"(id, enterprise_id, base_id, name, position, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{t08OtherTableID, t08ExternalTenantEnterpriseID, t08OtherBaseID, "隔离表", 0, timestamp},
		},
		{
			"INSERT INTO excel_structure_fields " +
				"(id, table_id, name, type, config, is_primary, position, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{t08SharedFieldID, t08SharedTableID, "标题", "FIELD_TYPE_TEXT", "{}", 1, 0, timestamp},
		},
		{
			"INSERT INTO excel_structure_fields " +
				"(id, table_id, name, type, config, is_primary, position, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{t08OtherFieldID, t08OtherTableID, "标题", "FIELD_TYPE_TEXT", "{}", 1, 0, timestamp},
		},
		{
			// 对测企业成员被授为 Base 可编辑协作者（缺陷 #13 的 peer 形态）。
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'editor', 0, 'invite', ?)",
			[]any{t08SharedBaseID, t08ForeignEditorID, timestamp},
		},
		{
			// 主 Base：可填写链接（anyone+filler，链接访客无企业归属）。
			"INSERT INTO excel_share_links " +
				"(base_id, token, scope, role, enabled, url, updated_at) VALUES (?, ?, 'anyone', 'filler', 1, '', ?)",
			[]any{t08SharedBaseID, t08FillerLinkToken, timestamp},
		},
		{
			// 其它 Base：可查看链接。
			"INSERT INTO excel_share_links " +
				"(base_id, token, scope, role, enabled, url, updated_at) VALUES (?, ?, 'anyone', 'viewer', 1, '', ?)",
			[]any{t08OtherBaseID, t08ViewerLinkToken, timestamp},
		},
	}
	for _, fixture := range fixtures {
		if _, err := db.ExecContext(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatalf("seed external tenant fixture %q: %v", fixture.query, err)
		}
	}
	repository := base.NewRecordsRepository(db)
	for _, row := range []base.Record{
		{
			ID:      t08OwnerRowID,
			TableID: t08SharedTableID,
			Cells: map[string]*commonv1.Value{
				t08SharedFieldID: collaboratorStringValue("所有者的行"),
			},
			CreatedBy: t08OwnerUserID,
			CreatedAt: timestamp,
			Version:   1,
		},
		{
			ID:      t08OtherRowID,
			TableID: t08OtherTableID,
			Cells: map[string]*commonv1.Value{
				t08OtherFieldID: collaboratorStringValue("其它 Base 的行"),
			},
			CreatedBy: t08OwnerUserID,
			CreatedAt: timestamp,
			Version:   1,
		},
	} {
		tx, err := repository.BeginTx(ctx)
		if err != nil {
			t.Fatalf("seed row begin tx: %v", err)
		}
		if err := repository.CreateRecordTx(ctx, tx, row); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed row %s: %v", row.ID, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("seed row %s commit: %v", row.ID, err)
		}
	}
}

// t08PrincipalContext mirrors the gateway call shape for one principal with an
// explicit enterprise affiliation and link context.
func t08PrincipalContext(
	t *testing.T,
	principal *commonv1.Principal,
	rowScope sharev1.RowScope,
	baseID string,
) context.Context {
	t.Helper()
	payload, err := proto.Marshal(principal)
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

func t08ForeignContext(t *testing.T, userID string, rowScope sharev1.RowScope, baseID string) context.Context {
	t.Helper()
	return t08PrincipalContext(t, &commonv1.Principal{
		UserId:       userID,
		EnterpriseId: t08ForeignEnterpriseID,
		Internal:     false,
	}, rowScope, baseID)
}

func t08LinkContext(
	t *testing.T,
	userID, token, role string,
	rowScope sharev1.RowScope,
	baseID string,
) context.Context {
	t.Helper()
	return t08PrincipalContext(t, &commonv1.Principal{
		UserId: userID,
		LinkContext: &commonv1.LinkContext{
			BaseId:    baseID,
			LinkToken: token,
			Role:      role,
		},
	}, rowScope, baseID)
}

// t08ConflictData extracts the 4001 detail_map（their_value/their_version）.
func t08ConflictData(t *testing.T, err error) map[string]any {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want 4001 conflict data")
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	for _, detail := range grpcStatus.Details() {
		if errorDetail, ok := detail.(*commonv1.ErrorDetail); ok &&
			errorDetail.GetDetailMap() != nil {
			return errorDetail.GetDetailMap().AsMap()
		}
	}
	t.Fatalf("error %v carries no ErrorDetail data", err)
	return nil
}

// TestForeignEnterpriseCollaboratorReadsAndWritesRecords 复现缺陷 #13 的 peer
// 链路：带对测企业归属的显式可编辑协作者在授权 Base 内 H-49/H-48 读取返回 0
// 且行级范围正确；H-51 写入成功、陈旧版本 4001（附对方新值）、无授权 3001。
func TestForeignEnterpriseCollaboratorReadsAndWritesRecords(t *testing.T) {
	harness := newExternalTenantHarness(t)
	ctx := context.Background()

	decision, err := harness.share.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{
			UserId:       t08ForeignEditorID,
			EnterpriseId: t08ForeignEnterpriseID,
		},
		BaseId: t08SharedBaseID,
		Action: sharev1.Action_ACTION_EDIT_ALL,
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

	editorCtx := t08ForeignContext(t, t08ForeignEditorID, decision.GetRowScope(), t08SharedBaseID)

	// H-49 单条读取：修复前 TableByID(对测企业, table) 落空 → 4002。
	read, err := harness.records.GetRecord(editorCtx, &recordsv1.GetRecordRequest{
		RecordId: t08OwnerRowID,
		BaseId:   t08SharedBaseID,
	})
	if err != nil {
		t.Fatalf("GetRecord(foreign editor) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if read.GetRecord().GetId() != t08OwnerRowID ||
		read.GetRecord().GetCellsMap()[t08SharedFieldID].GetStringValue() != "所有者的行" {
		t.Fatalf("GetRecord(foreign editor) = %+v, want owner row", read.GetRecord())
	}

	// H-48 列表：行级 ALL 应看到全部行（不 4002、不漏行）。
	listed, err := harness.records.QueryView(editorCtx, &recordsv1.QueryViewRequest{
		BaseId:  t08SharedBaseID,
		TableId: t08SharedTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(foreign editor) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if len(listed.GetRecordList()) != 1 || listed.GetRecordList()[0].GetId() != t08OwnerRowID {
		t.Fatalf("QueryView(foreign editor) = %d rows, want the owner row", len(listed.GetRecordList()))
	}

	// H-51 乐观锁写入成功。
	updated, err := harness.records.UpdateCell(editorCtx, &recordsv1.UpdateCellRequest{
		RecordId:      t08OwnerRowID,
		FieldId:       t08SharedFieldID,
		Value:         collaboratorStringValue("外部编辑-改"),
		ExpectVersion: 1,
	})
	if err != nil {
		t.Fatalf("UpdateCell(foreign editor) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if updated.GetVersion() != 2 {
		t.Fatalf("UpdateCell(foreign editor) version = %d, want 2", updated.GetVersion())
	}

	// 陈旧版本 → 4001，附对方新值/新版本（票 02 键位 their_value/their_version）。
	_, staleErr := harness.records.UpdateCell(editorCtx, &recordsv1.UpdateCellRequest{
		RecordId:      t08OwnerRowID,
		FieldId:       t08SharedFieldID,
		Value:         collaboratorStringValue("外部编辑-陈旧"),
		ExpectVersion: 1,
	})
	if staleErr == nil {
		t.Fatal("UpdateCell(stale) error = nil, want business code 4001")
	}
	if collaboratorBusinessCode(t, staleErr) != deal.CodeConcurrentConflict {
		t.Fatalf("UpdateCell(stale) error = %v, want business code 4001", staleErr)
	}
	conflict := t08ConflictData(t, staleErr)
	if conflict["their_version"] != float64(2) {
		t.Fatalf("conflict their_version = %v, want 2", conflict["their_version"])
	}

	// 无任何 Base 授权的对测企业账号：读写都 fail-closed 3001。
	strangerCtx := t08ForeignContext(t, t08ForeignStrangerID, sharev1.RowScope_ROW_SCOPE_ALL, t08SharedBaseID)
	if _, err := harness.records.GetRecord(strangerCtx, &recordsv1.GetRecordRequest{
		RecordId: t08OwnerRowID,
		BaseId:   t08SharedBaseID,
	}); collaboratorBusinessCode(t, err) != deal.CodePermissionDenied {
		t.Fatalf("GetRecord(foreign stranger) error = %v, want business code 3001", err)
	}
	if _, err := harness.records.UpdateCell(strangerCtx, &recordsv1.UpdateCellRequest{
		RecordId:      t08OwnerRowID,
		FieldId:       t08SharedFieldID,
		Value:         collaboratorStringValue("越权写入"),
		ExpectVersion: 2,
	}); collaboratorBusinessCode(t, err) != deal.CodePermissionDenied {
		t.Fatalf("UpdateCell(foreign stranger) error = %v, want business code 3001", err)
	}

	// 写入结果未被越权请求破坏。
	after, err := harness.records.GetRecord(editorCtx, &recordsv1.GetRecordRequest{
		RecordId: t08OwnerRowID,
		BaseId:   t08SharedBaseID,
	})
	if err != nil {
		t.Fatalf("GetRecord(after stranger attempts) error = %v", err)
	}
	if after.GetRecord().GetVersion() != 2 ||
		after.GetRecord().GetCellsMap()[t08SharedFieldID].GetStringValue() != "外部编辑-改" {
		t.Fatalf("owner row drifted after stranger attempts: %+v", after.GetRecord())
	}
}

// TestForeignEnterpriseCollaboratorCrossBaseDenied 锁 fail-closed 边界：在
// Base1 有授权的外部协作者，持 Base2 的 record_id/table_id 读取或写入一律
// 3001（不返回数据、不误报 4002、不 9001）。
func TestForeignEnterpriseCollaboratorCrossBaseDenied(t *testing.T) {
	harness := newExternalTenantHarness(t)

	editorCtx := t08ForeignContext(t, t08ForeignEditorID, sharev1.RowScope_ROW_SCOPE_ALL, t08OtherBaseID)
	if _, err := harness.records.GetRecord(editorCtx, &recordsv1.GetRecordRequest{
		RecordId: t08OtherRowID,
		BaseId:   t08OtherBaseID,
	}); collaboratorBusinessCode(t, err) != deal.CodePermissionDenied {
		t.Fatalf("GetRecord(cross base) error = %v, want business code 3001", err)
	}
	if _, err := harness.records.UpdateCell(editorCtx, &recordsv1.UpdateCellRequest{
		RecordId:      t08OtherRowID,
		FieldId:       t08OtherFieldID,
		Value:         collaboratorStringValue("跨 Base 写入"),
		ExpectVersion: 1,
	}); collaboratorBusinessCode(t, err) != deal.CodePermissionDenied {
		t.Fatalf("UpdateCell(cross base) error = %v, want business code 3001", err)
	}
}

// TestLinkSessionRecordsUnchanged 锁链接会话不回归：可填写链接（OWN）只看得
// 见自己创建的行并能在其上写入；可查看链接（ALL）读取全表；链接上下文参与
// Base 裁决（无效 token 的链接上下文 3001）。
func TestLinkSessionRecordsUnchanged(t *testing.T) {
	harness := newExternalTenantHarness(t)

	// 可填写链接：OWN 行级范围，看不到所有者的行。
	fillerCtx := t08LinkContext(t, t08LinkUserID, t08FillerLinkToken, "filler",
		sharev1.RowScope_ROW_SCOPE_OWN, t08SharedBaseID)
	listed, err := harness.records.QueryView(fillerCtx, &recordsv1.QueryViewRequest{
		BaseId:  t08SharedBaseID,
		TableId: t08SharedTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(filler link) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if len(listed.GetRecordList()) != 0 {
		t.Fatalf("QueryView(filler link) = %d rows, want 0（OWN 隐藏他人记录）", len(listed.GetRecordList()))
	}
	if _, err := harness.records.GetRecord(fillerCtx, &recordsv1.GetRecordRequest{
		RecordId: t08OwnerRowID,
		BaseId:   t08SharedBaseID,
	}); collaboratorBusinessCode(t, err) != deal.CodeDeletedResource {
		t.Fatalf("GetRecord(filler link, owner row) error = %v, want business code 4002", err)
	}

	created, err := harness.records.CreateRecord(fillerCtx, &recordsv1.CreateRecordRequest{
		TableId: t08SharedTableID,
		CellsMap: map[string]*commonv1.Value{
			t08SharedFieldID: collaboratorStringValue("链接访客填报"),
		},
	})
	if err != nil {
		t.Fatalf("CreateRecord(filler link) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	ownRead, err := harness.records.GetRecord(fillerCtx, &recordsv1.GetRecordRequest{
		RecordId: created.GetRecord().GetId(),
		BaseId:   t08SharedBaseID,
	})
	if err != nil {
		t.Fatalf("GetRecord(filler link, own row) error = %v", err)
	}
	if ownRead.GetRecord().GetCreatedBy() != t08LinkUserID {
		t.Fatalf("CreateRecord(filler link) created_by = %q, want link user", ownRead.GetRecord().GetCreatedBy())
	}
	ownWrite, err := harness.records.UpdateCell(fillerCtx, &recordsv1.UpdateCellRequest{
		RecordId:      created.GetRecord().GetId(),
		FieldId:       t08SharedFieldID,
		Value:         collaboratorStringValue("链接访客改自己的行"),
		ExpectVersion: 1,
	})
	if err != nil || ownWrite.GetVersion() != 2 {
		t.Fatalf("UpdateCell(filler link, own row) = %v, want version 2", err)
	}

	// 可查看链接：ALL 行级范围，读取全部行。
	viewerCtx := t08LinkContext(t, t08LinkUserID, t08ViewerLinkToken, "viewer",
		sharev1.RowScope_ROW_SCOPE_ALL, t08OtherBaseID)
	otherListed, err := harness.records.QueryView(viewerCtx, &recordsv1.QueryViewRequest{
		BaseId:  t08OtherBaseID,
		TableId: t08OtherTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(viewer link) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	if len(otherListed.GetRecordList()) != 1 ||
		otherListed.GetRecordList()[0].GetId() != t08OtherRowID {
		t.Fatalf("QueryView(viewer link) = %d rows, want the other-base row", len(otherListed.GetRecordList()))
	}

	// 伪造 token 的链接上下文：3001（fail-closed）。
	forgedCtx := t08LinkContext(t, t08LinkUserID, "t08-forged-token", "filler",
		sharev1.RowScope_ROW_SCOPE_OWN, t08SharedBaseID)
	if _, err := harness.records.GetRecord(forgedCtx, &recordsv1.GetRecordRequest{
		RecordId: created.GetRecord().GetId(),
		BaseId:   t08SharedBaseID,
	}); collaboratorBusinessCode(t, err) != deal.CodePermissionDenied {
		t.Fatalf("GetRecord(forged link) error = %v, want business code 3001", err)
	}
}

// TestInternalMemberTenantUnchangedByForeignCollaborators 补内部成员对照：
// 所属企业与 Base 一致的成员路径保持企业租户断言，行为与修复前完全一致。
func TestInternalMemberTenantUnchangedByForeignCollaborators(t *testing.T) {
	harness := newExternalTenantHarness(t)

	memberCtx := t08PrincipalContext(t, &commonv1.Principal{
		UserId:       t08OwnerUserID,
		EnterpriseId: t08ExternalTenantEnterpriseID,
		Internal:     true,
	}, sharev1.RowScope_ROW_SCOPE_ALL, t08SharedBaseID)

	created, err := harness.records.CreateRecord(memberCtx, &recordsv1.CreateRecordRequest{
		TableId: t08SharedTableID,
		CellsMap: map[string]*commonv1.Value{
			t08SharedFieldID: collaboratorStringValue("内部成员新行"),
		},
	})
	if err != nil {
		t.Fatalf("CreateRecord(internal member) error = %v (business code %d)", err, collaboratorBusinessCode(t, err))
	}
	listed, err := harness.records.QueryView(memberCtx, &recordsv1.QueryViewRequest{
		BaseId:  t08SharedBaseID,
		TableId: t08SharedTableID,
	})
	if err != nil {
		t.Fatalf("QueryView(internal member) error = %v", err)
	}
	if len(listed.GetRecordList()) != 2 {
		t.Fatalf("QueryView(internal member) = %d rows, want 2", len(listed.GetRecordList()))
	}
	updated, err := harness.records.UpdateCell(memberCtx, &recordsv1.UpdateCellRequest{
		RecordId:      created.GetRecord().GetId(),
		FieldId:       t08SharedFieldID,
		Value:         collaboratorStringValue("内部成员改"),
		ExpectVersion: 1,
	})
	if err != nil || updated.GetVersion() != 2 {
		t.Fatalf("UpdateCell(internal member) = %v, want version 2", err)
	}

	// 内部成员访问其它 Base 的数据同样按 Base 企业命中（同企业）。
	otherRead, err := harness.records.GetRecord(memberCtx, &recordsv1.GetRecordRequest{
		RecordId: t08OtherRowID,
		BaseId:   t08OtherBaseID,
	})
	if err != nil {
		t.Fatalf("GetRecord(internal member, other base) error = %v", err)
	}
	if otherRead.GetRecord().GetId() != t08OtherRowID {
		t.Fatalf("GetRecord(internal member, other base) = %+v", otherRead.GetRecord())
	}
}
