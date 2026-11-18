// structure/foreign_collaborator_tenancy_test.go：结构域外部协作者租户契约
// （票 fix/p1p2-structure-external-tenancy，缺陷 TC-090-03..07 WS 握手探针）。
// 锁定「结构只读租户按数据表所属 Base 的企业解析，而不是按 Principal 自身
// enterprise_id」（spec §4，ADR 0004，与记录域缺陷 #13 同口径）：带对测企业
// 归属的外部 Base 协作者（WS 握手 peer 形态：CheckAccess 放行 → ListFields
// x-principal 探针）全部只读入口可用，且解析出 Base 后必须过 ReadAccess seam
// （fail-closed）；无授权的对测企业账号 3001、不存在的表 4002（ADR 0003）；
// 内部成员快路径与结构写路径（managerPrincipal + 企业过滤 TableByID）不回归。
// 夹具 ID 一律以 01JTFS 前缀命名并按前缀清理，不触碰其它夹具数据。
package structure_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"excel-libs/testmysql"
	commonv1 "excel/api/common/v1"
	sharev1 "excel/api/share/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
	"excel_core_service/internal/iam"
	"excel_core_service/internal/share"
	"excel_core_service/internal/structure"
	"excel_core_service/internal/workspace"
)

const (
	// tfxTenantEnterpriseID 是 Base 所属企业（数据租户，沙箱企业）。
	tfxTenantEnterpriseID = "01JTFSENTA0000000000000000"
	// tfxForeignEnterpriseID 是外部协作者自带的「对测」企业（与 Base 不同企业）。
	tfxForeignEnterpriseID = "01JTFSENTB0000000000000000"
	// tfxSharedBaseID / tfxOtherBaseID 是两个互不授权的 Base。
	tfxSharedBaseID = "01JTFSBASE1000000000000000"
	tfxOtherBaseID  = "01JTFSBASE2000000000000000"
	// tfxSharedTableID / tfxOtherTableID 属于对应 Base；tfxMissingTableID 不存在。
	tfxSharedTableID = "01JTFSTAB10000000000000000"
	tfxOtherTableID  = "01JTFSTAB20000000000000000"
	tfxMissingTabID  = "01JTFSMISS0000000000000000"
	// tfxSharedFieldID 属于 tfxSharedTableID。
	tfxSharedFieldID = "01JTFSFLD10000000000000000"
	// tfxSharedViewID / tfxOtherViewID 是两张表各自的默认视图。
	tfxSharedViewID = "01JTFSVIEW1000000000000000"
	tfxOtherViewID  = "01JTFSVIEW2000000000000000"
	// tfxOwnerUserID 是 Base 所属企业的所有者（内部成员快路径对照组）。
	tfxOwnerUserID = "01JTFSOWN10000000000000000"
	// tfxForeignEditorID 是带对测企业归属的显式外部可编辑协作者（WS 握手 peer）。
	tfxForeignEditorID = "01JTFSFEDT0000000000000000"
	// tfxForeignStrangerID 带对测企业归属但无任何 Base 授权。
	tfxForeignStrangerID = "01JTFSFSTR0000000000000000"
)

// tfxCountingReadAccess 包装生产 share seam 并记录裁决过的 Base：验证协作者
// 回退路径上 ReadAccess seam 被真实调用（ADR 0004：解析出 Base 后必须过 seam）。
type tfxCountingReadAccess struct {
	inner structure.ReadAccess
	calls []string
}

func (access *tfxCountingReadAccess) AllowRead(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
) error {
	access.calls = append(access.calls, baseID)
	return access.inner.AllowRead(ctx, principal, baseID)
}

type tfxStructureHarness struct {
	structure  structurev1.StructureServiceClient
	share      sharev1.ShareServiceClient
	db         *sql.DB
	readAccess *tfxCountingReadAccess
}

// newTfxStructureHarness 按 cmd/main.go 生产装配接线（真实 share/IAM/workspace
// 适配器 + ReadAccess seam），夹具只含 01JTFS 前缀行，清理按前缀精确删除，
// 不触碰共享表其它数据（共享 MySQL 上可能有其它套件在跑）。
func newTfxStructureHarness(t *testing.T) *tfxStructureHarness {
	t.Helper()
	db := testmysql.OpenTestDB(t, testMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetTfxStructureData(t, db)
	t.Cleanup(func() { resetTfxStructureData(t, db) })

	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	seedTfxStructureFixtures(t, db, now)

	workspaceRepository := base.NewWorkspaceRepository(db)
	iamRepository := base.NewIAMRepository(db)
	shareService, err := share.NewService(share.ServiceDeps{
		Repository:           base.NewShareRepository(db),
		Outbox:               bus.NewOutbox(base.NewBusStore(db)),
		Bases:                workspace.NewShareBaseResolver(workspaceRepository),
		BaseNames:            workspace.NewShareBaseResolver(workspaceRepository),
		ImportLock:           workspace.NewShareBaseResolver(workspaceRepository),
		BaseOwner:            workspace.NewShareBaseOwnerProvider(workspaceRepository),
		Members:              iam.NewMemberRoleProvider(iamRepository),
		EnterpriseDirectory:  iam.NewEnterpriseDirectoryProvider(iamRepository),
		UserLookup:           iam.NewShareUserLookup(iamRepository),
		Profiles:             iam.NewShareUserLookup(iamRepository),
		PermissionConfigPath: tfxPermissionPolicy(t),
	})
	if err != nil {
		t.Fatalf("share.NewService() error = %v", err)
	}

	structureRepository := base.NewStructureRepository(db)
	readAccess := &tfxCountingReadAccess{inner: structure.NewShareReadAccess(shareService)}
	structureService := structure.NewService(
		structureRepository,
		iam.NewMemberRoleProvider(iamRepository),
		nil,
		nil,
		func() time.Time { return now },
		readAccess,
		structure.ServiceOptions{Outbox: bus.NewOutbox(base.NewBusStore(db))},
	)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	share.Register(server, shareService)
	structure.Register(server, structureService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///tfx-structure-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &tfxStructureHarness{
		structure:  structurev1.NewStructureServiceClient(connection),
		share:      sharev1.NewShareServiceClient(connection),
		db:         db,
		readAccess: readAccess,
	}
}

// resetTfxStructureData 只清理 01JTFS 前缀夹具，不整表清空。
func resetTfxStructureData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_structure_field_options WHERE field_id LIKE '01JTFS%'",
		"DELETE FROM excel_structure_views WHERE id LIKE '01JTFS%' OR table_id LIKE '01JTFS%'",
		"DELETE FROM excel_structure_fields WHERE table_id LIKE '01JTFS%'",
		"DELETE FROM excel_structure_tables WHERE base_id LIKE '01JTFS%'",
		"DELETE FROM excel_share_collaborators WHERE base_id LIKE '01JTFS%' OR user_id LIKE '01JTFS%'",
		"DELETE FROM excel_share_links WHERE base_id LIKE '01JTFS%'",
		"DELETE FROM excel_bus_outbox WHERE base_id LIKE '01JTFS%'",
		"DELETE FROM excel_iam_members WHERE enterprise_id LIKE '01JTFS%'",
		"DELETE FROM excel_workspace_stars WHERE base_id LIKE '01JTFS%'",
		"DELETE FROM excel_workspace_recent WHERE base_id LIKE '01JTFS%'",
		"DELETE FROM excel_workspace_bases WHERE id LIKE '01JTFS%'",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset tfx structure data with %q: %v", statement, err)
		}
	}
}

func seedTfxStructureFixtures(t *testing.T, db *sql.DB, now time.Time) {
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
			[]any{tfxSharedBaseID, tfxTenantEnterpriseID, tfxOwnerUserID, "结构租户-共享 Base", timestamp, tfxOwnerUserID, timestamp},
		},
		{
			"INSERT INTO excel_workspace_bases " +
				"(id, enterprise_id, owner_id, name, last_modified_at, last_modifier, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{tfxOtherBaseID, tfxTenantEnterpriseID, tfxOwnerUserID, "结构租户-其它 Base", timestamp, tfxOwnerUserID, timestamp},
		},
		{
			"INSERT INTO excel_structure_tables " +
				"(id, enterprise_id, base_id, name, position, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{tfxSharedTableID, tfxTenantEnterpriseID, tfxSharedBaseID, "共享表", 0, timestamp},
		},
		{
			"INSERT INTO excel_structure_tables " +
				"(id, enterprise_id, base_id, name, position, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{tfxOtherTableID, tfxTenantEnterpriseID, tfxOtherBaseID, "隔离表", 0, timestamp},
		},
		{
			"INSERT INTO excel_structure_fields " +
				"(id, table_id, name, type, config, is_primary, position, created_at) " +
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			[]any{tfxSharedFieldID, tfxSharedTableID, "标题", "FIELD_TYPE_TEXT", "{}", 1, 0, timestamp},
		},
		{
			// 对测企业成员在其自己企业内的身份（readPrincipal 的成员角色校验对象）。
			"INSERT INTO excel_iam_members " +
				"(enterprise_id, user_id, role, status, joined_at) VALUES (?, ?, 'owner', 'active', ?)",
			[]any{tfxTenantEnterpriseID, tfxOwnerUserID, timestamp},
		},
		{
			"INSERT INTO excel_iam_members " +
				"(enterprise_id, user_id, role, status, joined_at) VALUES (?, ?, 'owner', 'active', ?)",
			[]any{tfxForeignEnterpriseID, tfxForeignEditorID, timestamp},
		},
		{
			"INSERT INTO excel_iam_members " +
				"(enterprise_id, user_id, role, status, joined_at) VALUES (?, ?, 'owner', 'active', ?)",
			[]any{tfxForeignEnterpriseID, tfxForeignStrangerID, timestamp},
		},
		{
			// 对测企业成员被授为共享 Base 可编辑协作者（TC-090-03..07 的 peer 形态）。
			"INSERT INTO excel_share_collaborators " +
				"(base_id, user_id, role, internal, source, granted_at) VALUES (?, ?, 'editor', 0, 'invite', ?)",
			[]any{tfxSharedBaseID, tfxForeignEditorID, timestamp},
		},
	}
	for _, fixture := range fixtures {
		if _, err := db.ExecContext(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatalf("seed tfx structure fixture %q: %v", fixture.query, err)
		}
	}
	for _, view := range []base.StructureView{
		{ID: tfxSharedViewID, TableID: tfxSharedTableID, Name: "表格", Type: "table", Config: []byte("{}"), Position: 0, CreatedAt: timestamp},
		{ID: tfxOtherViewID, TableID: tfxOtherTableID, Name: "表格", Type: "table", Config: []byte("{}"), Position: 0, CreatedAt: timestamp},
	} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("seed tfx view begin tx: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO excel_structure_views (id, table_id, name, type, config, position, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, view.ID, view.TableID, view.Name, view.Type, view.Config, view.Position, view.CreatedAt); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed tfx view %s: %v", view.ID, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("seed tfx view %s commit: %v", view.ID, err)
		}
	}
}

// tfxPermissionPolicy 与记录域 writeCollaboratorPermissionPolicy 同一份策略，
// 供真实 share.CheckAccess 裁决 VIEW。
func tfxPermissionPolicy(t *testing.T) string {
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

// tfxContext mirrors the gateway x-principal metadata transport（WS 探针同形）.
func tfxContext(t *testing.T, userID, enterpriseID string, internal bool) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     internal,
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
}

func tfxForeignContext(t *testing.T, userID string) context.Context {
	t.Helper()
	return tfxContext(t, userID, tfxForeignEnterpriseID, false)
}

func tfxOwnerContext(t *testing.T) context.Context {
	t.Helper()
	return tfxContext(t, tfxOwnerUserID, tfxTenantEnterpriseID, true)
}

// TestTfxForeignCollaboratorReadsStructure 复现 TC-090-03..07 握手链路：带对测
// 企业归属的 Base 可编辑协作者 CheckAccess(VIEW) 放行后，ListFields x-principal
// 探针（修复前 4002「记录/字段已被删除」）与其余只读入口全部放行，且每个入口
// 解析出 Base 后都真实经过 ReadAccess seam（ADR 0004 fail-closed 原则）。
func TestTfxForeignCollaboratorReadsStructure(t *testing.T) {
	harness := newTfxStructureHarness(t)
	editorCtx := tfxForeignContext(t, tfxForeignEditorID)

	// 握手第一步：share.CheckAccess(VIEW) 对外企业协作者放行（base 级路由）。
	decision, err := harness.share.CheckAccess(context.Background(), &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{UserId: tfxForeignEditorID, EnterpriseId: tfxForeignEnterpriseID},
		BaseId:    tfxSharedBaseID,
		Action:    sharev1.Action_ACTION_VIEW,
	})
	if err != nil {
		t.Fatalf("CheckAccess(VIEW) error = %v", err)
	}
	if !decision.GetAllow() {
		t.Fatalf("CheckAccess(VIEW) = allow %v role %v, want allow", decision.GetAllow(), decision.GetRole())
	}

	// 握手第二步：ListFields 探针（x-principal metadata，修复前 4002）。
	fields, err := harness.structure.ListFields(editorCtx, &structurev1.ListFieldsRequest{
		TableId: tfxSharedTableID,
	})
	if err != nil {
		t.Fatalf("ListFields(foreign editor) error = %v", err)
	}
	if len(fields.GetFieldList()) != 1 || fields.GetFieldList()[0].GetId() != tfxSharedFieldID {
		t.Fatalf("ListFields(foreign editor) = %v, want the primary field", fields.GetFieldList())
	}
	if len(harness.readAccess.calls) != 1 || harness.readAccess.calls[0] != tfxSharedBaseID {
		t.Fatalf("ReadAccess calls after ListFields = %v, want [%s]", harness.readAccess.calls, tfxSharedBaseID)
	}

	// 其余只读入口同口径：ListTables 协作者回退 + ListViews/GetViewConfig。
	tables, err := harness.structure.ListTables(editorCtx, &structurev1.ListTablesRequest{
		BaseId: tfxSharedBaseID,
	})
	if err != nil {
		t.Fatalf("ListTables(foreign editor) error = %v", err)
	}
	if len(tables.GetTableList()) != 1 || tables.GetTableList()[0].GetId() != tfxSharedTableID {
		t.Fatalf("ListTables(foreign editor) = %v, want the shared table", tables.GetTableList())
	}

	views, err := harness.structure.ListViews(editorCtx, &structurev1.ListViewsRequest{
		TableId: tfxSharedTableID,
	})
	if err != nil {
		t.Fatalf("ListViews(foreign editor) error = %v", err)
	}
	if len(views.GetViewList()) != 1 || views.GetViewList()[0].GetId() != tfxSharedViewID {
		t.Fatalf("ListViews(foreign editor) = %v, want the default view", views.GetViewList())
	}

	if _, err := harness.structure.GetViewConfig(editorCtx, &structurev1.GetViewConfigRequest{
		ViewId: tfxSharedViewID,
	}); err != nil {
		t.Fatalf("GetViewConfig(foreign editor) error = %v", err)
	}

	// 四个只读入口各过一次 seam，全部落在共享 Base 上。
	if len(harness.readAccess.calls) != 4 {
		t.Fatalf("ReadAccess calls = %v, want one per read endpoint", harness.readAccess.calls)
	}
	for _, baseID := range harness.readAccess.calls {
		if baseID != tfxSharedBaseID {
			t.Fatalf("ReadAccess base = %q, want %q", baseID, tfxSharedBaseID)
		}
	}
}

// TestTfxForeignStrangerDeniedStructureReads 锁 fail-closed 与 ADR 0003 语义：
// 对测企业无授权账号（非协作者）读已存在表 3001（不是 4002）；不存在的表 4002；
// 已授权协作者持其它 Base 的 table_id/view_id 一律 3001。
func TestTfxForeignStrangerDeniedStructureReads(t *testing.T) {
	harness := newTfxStructureHarness(t)
	strangerCtx := tfxForeignContext(t, tfxForeignStrangerID)

	if _, err := harness.structure.ListFields(strangerCtx, &structurev1.ListFieldsRequest{
		TableId: tfxSharedTableID,
	}); err == nil {
		t.Fatal("ListFields(foreign stranger) error = nil, want business code 3001")
	} else {
		requireErrorCode(t, err, deal.CodePermissionDenied)
	}
	if _, err := harness.structure.ListTables(strangerCtx, &structurev1.ListTablesRequest{
		BaseId: tfxSharedBaseID,
	}); err == nil {
		t.Fatal("ListTables(foreign stranger) error = nil, want business code 3001")
	} else {
		requireErrorCode(t, err, deal.CodePermissionDenied)
	}
	if _, err := harness.structure.ListViews(strangerCtx, &structurev1.ListViewsRequest{
		TableId: tfxSharedTableID,
	}); err == nil {
		t.Fatal("ListViews(foreign stranger) error = nil, want business code 3001")
	} else {
		requireErrorCode(t, err, deal.CodePermissionDenied)
	}
	if _, err := harness.structure.GetViewConfig(strangerCtx, &structurev1.GetViewConfigRequest{
		ViewId: tfxSharedViewID,
	}); err == nil {
		t.Fatal("GetViewConfig(foreign stranger) error = nil, want business code 3001")
	} else {
		requireErrorCode(t, err, deal.CodePermissionDenied)
	}

	// 不存在的表：仍 4002（不与无权限混淆，ADR 0003 口径）。
	_, err := harness.structure.ListFields(strangerCtx, &structurev1.ListFieldsRequest{
		TableId: tfxMissingTabID,
	})
	requireErrorCode(t, err, deal.CodeDeletedResource)

	// 跨 Base：共享 Base 的协作者持其它 Base 的 table_id/view_id → 3001。
	editorCtx := tfxForeignContext(t, tfxForeignEditorID)
	_, crossFieldsErr := harness.structure.ListFields(editorCtx, &structurev1.ListFieldsRequest{
		TableId: tfxOtherTableID,
	})
	requireErrorCode(t, crossFieldsErr, deal.CodePermissionDenied)
	_, crossViewsErr := harness.structure.ListViews(editorCtx, &structurev1.ListViewsRequest{
		TableId: tfxOtherTableID,
	})
	requireErrorCode(t, crossViewsErr, deal.CodePermissionDenied)
	_, crossConfigErr := harness.structure.GetViewConfig(editorCtx, &structurev1.GetViewConfigRequest{
		ViewId: tfxOtherViewID,
	})
	requireErrorCode(t, crossConfigErr, deal.CodePermissionDenied)
}

// TestTfxInternalMemberAndWritePathUnchanged 补内部成员对照与写边界：内部成员
// 走自身企业快路径（seam 零调用）；对测企业协作者写结构仍被企业租户断言拒绝
// （manager 可判但 TableByID 按其企业查无此表 → 4002），表行完好。
func TestTfxInternalMemberAndWritePathUnchanged(t *testing.T) {
	harness := newTfxStructureHarness(t)
	ownerCtx := tfxOwnerContext(t)

	fields, err := harness.structure.ListFields(ownerCtx, &structurev1.ListFieldsRequest{
		TableId: tfxSharedTableID,
	})
	if err != nil {
		t.Fatalf("ListFields(internal owner) error = %v", err)
	}
	if len(fields.GetFieldList()) != 1 {
		t.Fatalf("ListFields(internal owner) = %v, want one field", fields.GetFieldList())
	}
	if len(harness.readAccess.calls) != 0 {
		t.Fatalf("ReadAccess calls for internal member = %v, want none", harness.readAccess.calls)
	}

	// 写路径不放宽：外企业协作者（其自企业 owner 可判 manager）删共享表被
	// 企业租户断言拒绝（4002），共享表行不被删除。
	editorCtx := tfxForeignContext(t, tfxForeignEditorID)
	_, err = harness.structure.DeleteTable(editorCtx, &structurev1.DeleteTableRequest{
		TableId: tfxSharedTableID,
		Confirm: true,
	})
	requireErrorCode(t, err, deal.CodeDeletedResource)

	after, err := harness.structure.ListFields(ownerCtx, &structurev1.ListFieldsRequest{
		TableId: tfxSharedTableID,
	})
	if err != nil || len(after.GetFieldList()) != 1 {
		t.Fatalf("shared table drifted after foreign write attempt: fields=%v err=%v", after.GetFieldList(), err)
	}
}
