// structure/duplicate_table_records_test.go：复制数据表（H-32/缺陷 #12）端到端
// 行为——记录随结构一起复制、评论不复制、记录按新记录语义生成、复制失败不留
// 半成品表。断言只走 RPC 外部缝（Testing Decisions），夹具 id 集中在 04 段。
package structure_test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"excel_core_service/base"
	corebus "excel_core_service/bus"
	"excel_core_service/internal/records"
	"excel_core_service/internal/structure"
)

const (
	duplicateEnterpriseID = "01J00000000000000000000904"
	duplicateOwnerID      = "01J00000000000000000004201"
	duplicateOtherID      = "01J00000000000000000004202"
	duplicateBaseID       = "01J00000000000000000004203"
	duplicateOptionTodoID = "01J0000000000000000000TO01"
	duplicateOptionDoneID = "01J0000000000000000000DN01"
)

func textCellValue(text string) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: text}}
}

func numberCellValue(number int64) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: number}}
}

// duplicateRoleProvider 与 structure_test 的 fakeRoleProvider 同形，但租户键取
// 复制夹具企业，避免与既有夹具互相覆盖。
type duplicateRoleProvider struct {
	roles map[string]iamv1.MemberRole
}

func (provider *duplicateRoleProvider) Role(
	_ context.Context,
	enterpriseID, userID string,
) (iamv1.MemberRole, error) {
	if enterpriseID != duplicateEnterpriseID {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, nil
	}
	return provider.roles[userID], nil
}

// fakeTableRecordsImpact 是可注入失败的记录复制缝；err 非空即模拟记录复制失败，
// 用于回滚路径。duplicator 可选注入真实缝以观察调用。
type fakeTableRecordsImpact struct {
	duplicator structure.TableRecordsDuplicator
	err        error
	calls      []string
}

func (impact *fakeTableRecordsImpact) AffectedRecords(
	context.Context,
	string,
	string,
) (int, error) {
	return 0, nil
}

func (impact *fakeTableRecordsImpact) DuplicateTableRecords(
	ctx context.Context,
	sourceTableID, targetTableID string,
) error {
	impact.calls = append(impact.calls, sourceTableID)
	if impact.err != nil {
		return impact.err
	}
	if impact.duplicator == nil {
		return nil
	}
	return impact.duplicator.DuplicateTableRecords(ctx, sourceTableID, targetTableID)
}

type duplicateRowScopeAll struct{}

func (duplicateRowScopeAll) RowScope(
	context.Context,
	*commonv1.Principal,
	string,
) (string, error) {
	return "ROW_SCOPE_ALL", nil
}

type duplicateTableHarness struct {
	client            structurev1.StructureServiceClient
	recordsClient     recordsv1.RecordsServiceClient
	db                *sql.DB
	structure         *base.StructureRepository
	recordsStore      *base.RecordsRepository
	calcRepository    *base.CalcRepository
	commentRepository *base.CommentRepository
	records           *fakeTableRecordsImpact
	now               time.Time
}

// newDuplicateTableHarness 构造复制数据表的 bufconn 测试服：structure 与 records
// 服务同时注册，记录/评论断言一律经 RPC 读回。recordsDuplicator 传 nil 用真实
// records 组合适配器，注入失败缝用于回滚路径。
func newDuplicateTableHarness(
	t *testing.T,
	recordsDuplicator structure.TableRecordsDuplicator,
) *duplicateTableHarness {
	t.Helper()
	db, err := base.OpenDB(testMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetDuplicateTableData(t, db)
	t.Cleanup(func() { resetDuplicateTableData(t, db) })

	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	structureRepository := base.NewStructureRepository(db)
	recordsRepository := base.NewRecordsRepository(db)
	calcRepository := base.NewCalcRepository(db)
	commentRepository := base.NewCommentRepository(db)
	roles := &duplicateRoleProvider{roles: map[string]iamv1.MemberRole{
		duplicateOwnerID: iamv1.MemberRole_MEMBER_ROLE_OWNER,
		duplicateOtherID: iamv1.MemberRole_MEMBER_ROLE_MEMBER,
	}}

	var impact structure.RecordsImpactCounter
	var recordsImpact *fakeTableRecordsImpact
	if recordsDuplicator == nil {
		impact = records.NewStructureImpactAdapter(
			recordsRepository,
			structureRepository,
			calcRepository,
			func() time.Time { return now },
		)
	} else {
		recordsImpact = &fakeTableRecordsImpact{duplicator: recordsDuplicator}
		impact = recordsImpact
	}

	structureService := structure.NewService(
		structureRepository,
		roles,
		impact,
		nil,
		func() time.Time { return now },
		nil,
		// DeleteTable 需要 outbox 落 table.deleted；复制流程不产事件，装配保持一致。
		structure.ServiceOptions{Outbox: corebus.NewOutbox(base.NewBusStore(db))},
	)
	recordsService := records.NewService(
		recordsRepository,
		records.NewStructureViewAccessor(structureRepository, nil),
		duplicateRowScopeAll{},
		records.NewCalcFormulaValueProvider(calcRepository),
		func() time.Time { return now },
		commentRepository,
	)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	structure.Register(server, structureService)
	records.Register(server, recordsService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &duplicateTableHarness{
		client:            structurev1.NewStructureServiceClient(conn),
		recordsClient:     recordsv1.NewRecordsServiceClient(conn),
		db:                db,
		structure:         structureRepository,
		recordsStore:      recordsRepository,
		calcRepository:    calcRepository,
		commentRepository: commentRepository,
		records:           recordsImpact,
		now:               now,
	}
}

// resetDuplicateTableData 按 04 段夹具企业清理结构/记录/评论/公式行，避免触碰
// 共库中其它夹具的数据。
func resetDuplicateTableData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_histories WHERE record_id IN (SELECT id FROM excel_records WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "'))",
		"DELETE FROM excel_calc_materialized WHERE record_id IN (SELECT id FROM excel_records WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "'))",
		"DELETE FROM excel_calc_jobs WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "')",
		"DELETE FROM excel_records WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "')",
		"DELETE FROM excel_comment_comments WHERE record_id IN (SELECT id FROM excel_records WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "'))",
		"DELETE FROM excel_structure_field_options WHERE field_id IN (SELECT id FROM excel_structure_fields WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "'))",
		"DELETE FROM excel_structure_views WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "')",
		"DELETE FROM excel_structure_fields WHERE table_id IN (SELECT id FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "')",
		"DELETE FROM excel_structure_tables WHERE enterprise_id = '" + duplicateEnterpriseID + "'",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset duplicate table data: %v", err)
		}
	}
}

// duplicateQueryContext 携带操作者身份与 records 读链要求的 x-row-scope 元数据。
func duplicateQueryContext(
	t *testing.T,
	userID string,
) context.Context {
	t.Helper()
	return metadata.AppendToOutgoingContext(
		authenticatedContext(t, userID, duplicateEnterpriseID),
		"x-row-scope",
		"ROW_SCOPE_ALL",
	)
}

// seedDuplicateRecord 经 records 仓储落一条源记录（夹具准备，断言仍走 RPC）。
func seedDuplicateRecord(
	t *testing.T,
	harness *duplicateTableHarness,
	tableID, createdBy string,
	createdAt time.Time,
	version int64,
	cells map[string]*commonv1.Value,
) base.Record {
	t.Helper()
	recordID, err := idgen.New()
	if err != nil {
		t.Fatalf("idgen.New() error = %v", err)
	}
	tx, err := harness.recordsStore.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("records BeginTx() error = %v", err)
	}
	record := base.Record{
		ID:        recordID,
		TableID:   tableID,
		Cells:     cells,
		CreatedBy: createdBy,
		CreatedAt: createdAt.UnixMilli(),
		Version:   version,
	}
	if err := harness.recordsStore.CreateRecordTx(context.Background(), tx, record); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seeded record: %v", err)
	}
	return record
}

// seedDuplicateComment 经 comment 仓储在源记录上落一条评论（夹具准备）。
func seedDuplicateComment(
	t *testing.T,
	harness *duplicateTableHarness,
	baseID, recordID, author, content string,
) {
	t.Helper()
	commentID, err := idgen.New()
	if err != nil {
		t.Fatalf("idgen.New() error = %v", err)
	}
	tx, err := harness.commentRepository.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("comment BeginTx() error = %v", err)
	}
	if err := harness.commentRepository.CreateTx(context.Background(), tx, base.Comment{
		ID:        commentID,
		RecordID:  recordID,
		Content:   content,
		Author:    author,
		Status:    "OPEN",
		CreatedAt: harness.now.Add(-time.Hour).UnixMilli(),
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed comment: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seeded comment: %v", err)
	}
}

// duplicateFieldIDsByName 经结构 RPC 读回字段清单并返回 name→id 投影。
func duplicateFieldIDsByName(
	t *testing.T,
	harness *duplicateTableHarness,
	tableID string,
) map[string]string {
	t.Helper()
	response, err := harness.client.ListFields(
		authenticatedContext(t, duplicateOwnerID, duplicateEnterpriseID),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	ids := make(map[string]string, len(response.GetFieldList()))
	for _, field := range response.GetFieldList() {
		ids[field.GetName()] = field.GetId()
	}
	return ids
}

// duplicateRecords 经 records RPC 读回表内记录（外部行为缝）。
func duplicateRecords(
	t *testing.T,
	harness *duplicateTableHarness,
	baseID, tableID string,
) []*recordsv1.Record {
	t.Helper()
	response, err := harness.recordsClient.QueryView(
		duplicateQueryContext(t, duplicateOwnerID),
		&recordsv1.QueryViewRequest{BaseId: baseID, TableId: tableID},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	return response.GetRecordList()
}

// duplicateUnsolvedCommentCounts 经 records RPC 读回页内未解决评论数。
func duplicateUnsolvedCommentCounts(
	t *testing.T,
	harness *duplicateTableHarness,
	baseID, tableID string,
) map[string]int32 {
	t.Helper()
	response, err := harness.recordsClient.QueryView(
		duplicateQueryContext(t, duplicateOwnerID),
		&recordsv1.QueryViewRequest{BaseId: baseID, TableId: tableID},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	return response.GetUnsolvedCommentCountMap()
}

// TestDuplicateTableCopiesRecordsWithNewRecordSemantics 验收基线（缺陷 #12）：
// 复制数据表后字段/选项/视图完整复制，记录数与逐格值与源一致，记录版本重置、
// created_by/created_at 按新记录语义生成，评论不跟随（副本评论为 0）。
func TestDuplicateTableCopiesRecordsWithNewRecordSemantics(t *testing.T) {
	harness := newDuplicateTableHarness(t, nil)
	ctx := authenticatedContext(t, duplicateOwnerID, duplicateEnterpriseID)
	sourceTable, err := harness.client.CreateTable(ctx, &structurev1.CreateTableRequest{
		BaseId: duplicateBaseID,
		Name:   "复制清单t04",
	})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	sourceTableID := sourceTable.GetTable().GetId()
	quantityField, err := harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: sourceTableID,
		Name:    "数量",
		Type:    structurev1.FieldType_FIELD_TYPE_NUMBER,
	})
	if err != nil {
		t.Fatalf("CreateField(数量) error = %v", err)
	}
	if quantityField.GetField().GetId() == "" {
		t.Fatal("CreateField(数量) returned empty field id")
	}
	if _, err := harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: sourceTableID,
		Name:    "阶段",
		Type:    structurev1.FieldType_FIELD_TYPE_SELECT,
		ConfigMap: configMap(t, map[string]any{
			"option_list": []any{
				map[string]any{"id": duplicateOptionTodoID, "name": "待办", "color": "#111111"},
				map[string]any{"id": duplicateOptionDoneID, "name": "完成", "color": "#222222"},
			},
		}),
	}); err != nil {
		t.Fatalf("CreateField(阶段) error = %v", err)
	}
	if _, err := harness.client.CreateView(ctx, &structurev1.CreateViewRequest{
		TableId: sourceTableID,
		Name:    "列表t04",
		Type:    "table",
	}); err != nil {
		t.Fatalf("CreateView(列表t04) error = %v", err)
	}
	// 视图配置：默认视图（表格）隐藏「阶段」字段，复制后应重映射到副本字段 id。
	sourceFields := duplicateFieldIDsByName(t, harness, sourceTableID)
	seededViews, err := harness.client.ListViews(ctx, &structurev1.ListViewsRequest{
		TableId: sourceTableID,
	})
	if err != nil {
		t.Fatalf("ListViews(source) error = %v", err)
	}
	var gridView *structurev1.View
	for _, view := range seededViews.GetViewList() {
		if view.GetName() == "表格" {
			gridView = view
			break
		}
	}
	if gridView == nil {
		t.Fatalf("seeded views missing grid view: %+v", seededViews.GetViewList())
	}
	if _, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: gridView.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			HiddenFieldList: []string{sourceFields["阶段"]},
		},
	}); err != nil {
		t.Fatalf("UpdateViewConfig() error = %v", err)
	}

	past := harness.now.Add(-time.Hour)
	recordA := seedDuplicateRecord(t, harness, sourceTableID, duplicateOtherID, past, 5, map[string]*commonv1.Value{
		sourceFields["标题"]: textCellValue("盘点A"),
		sourceFields["数量"]: numberCellValue(3),
		sourceFields["阶段"]: textCellValue(duplicateOptionTodoID),
	})
	seedDuplicateRecord(t, harness, sourceTableID, duplicateOtherID, past, 7, map[string]*commonv1.Value{
		sourceFields["标题"]: textCellValue("盘点B"),
		sourceFields["数量"]: numberCellValue(9),
		sourceFields["阶段"]: textCellValue(duplicateOptionDoneID),
	})
	seedDuplicateComment(t, harness, duplicateBaseID, recordA.ID, duplicateOtherID, "源记录评论")

	duplicated, err := harness.client.DuplicateTable(ctx, &structurev1.DuplicateTableRequest{
		TableId: sourceTableID,
	})
	if err != nil {
		t.Fatalf("DuplicateTable() error = %v", err)
	}
	copyTable := duplicated.GetTable()
	if copyTable.GetId() == sourceTableID {
		t.Fatal("DuplicateTable() reused the source table id")
	}
	if !strings.Contains(copyTable.GetName(), "副本") {
		t.Fatalf("DuplicateTable() name = %q, want 副本 suffix", copyTable.GetName())
	}

	// 字段与选项逐项一致（选项 id 由结构复制保留，记录单元格因此原值可用）。
	copyFields := duplicateFieldIDsByName(t, harness, copyTable.GetId())
	for _, name := range []string{"标题", "数量", "阶段"} {
		if _, ok := copyFields[name]; !ok {
			t.Fatalf("copy fields = %v, want %q copied", copyFields, name)
		}
	}
	sourceFieldList, err := harness.client.ListFields(ctx, &structurev1.ListFieldsRequest{TableId: sourceTableID})
	if err != nil {
		t.Fatalf("ListFields(source) error = %v", err)
	}
	for _, field := range sourceFieldList.GetFieldList() {
		copied := copyFieldByName(t, copyFields, field.GetName(), copyTable.GetId(), harness)
		if copied.GetType() != field.GetType() {
			t.Fatalf("copied field %q type = %v, want %v", field.GetName(), copied.GetType(), field.GetType())
		}
		if field.GetName() == "阶段" {
			wantOptions := field.GetConfigMap().GetFields()["option_list"]
			gotOptions := copied.GetConfigMap().GetFields()["option_list"]
			if !proto.Equal(wantOptions, gotOptions) {
				t.Fatalf("copied option_list = %v, want %v", gotOptions, wantOptions)
			}
		}
	}

	// 视图完整复制（含隐藏字段重映射）。
	sourceViews, err := harness.client.ListViews(ctx, &structurev1.ListViewsRequest{TableId: sourceTableID})
	if err != nil {
		t.Fatalf("ListViews(source) error = %v", err)
	}
	copyViews, err := harness.client.ListViews(ctx, &structurev1.ListViewsRequest{TableId: copyTable.GetId()})
	if err != nil {
		t.Fatalf("ListViews(copy) error = %v", err)
	}
	if len(copyViews.GetViewList()) != len(sourceViews.GetViewList()) || len(copyViews.GetViewList()) != 2 {
		t.Fatalf("copy views = %+v, want %d views", copyViews.GetViewList(), len(sourceViews.GetViewList()))
	}
	copyViewByName := make(map[string]*structurev1.View, len(copyViews.GetViewList()))
	for _, view := range copyViews.GetViewList() {
		copyViewByName[view.GetName()] = view
	}
	for _, sourceView := range sourceViews.GetViewList() {
		copied, ok := copyViewByName[sourceView.GetName()]
		if !ok {
			t.Fatalf("copy views miss %q: %+v", sourceView.GetName(), copyViews.GetViewList())
		}
		if copied.GetType() != sourceView.GetType() {
			t.Fatalf("copied view %q type = %q, want %q", sourceView.GetName(), copied.GetType(), sourceView.GetType())
		}
	}
	gridViewID := copyViewByName["表格"].GetId()
	copyConfig, err := harness.client.GetViewConfig(ctx, &structurev1.GetViewConfigRequest{ViewId: gridViewID})
	if err != nil {
		t.Fatalf("GetViewConfig(copy) error = %v", err)
	}
	hidden := copyConfig.GetViewConfig().GetHiddenFieldList()
	if len(hidden) != 1 || hidden[0] != copyFields["阶段"] {
		t.Fatalf("copied hidden_field_list = %v, want remapped [%s]", hidden, copyFields["阶段"])
	}

	// 记录数与逐格值一致；版本/创建人/时间按新记录语义生成。
	sourceRecords := duplicateRecords(t, harness, duplicateBaseID, sourceTableID)
	copyRecords := duplicateRecords(t, harness, duplicateBaseID, copyTable.GetId())
	if len(copyRecords) != len(sourceRecords) || len(copyRecords) != 2 {
		t.Fatalf("copied records = %d, source records = %d, want 2", len(copyRecords), len(sourceRecords))
	}
	copyByTitle := make(map[string]*recordsv1.Record, len(copyRecords))
	for _, record := range copyRecords {
		if record.GetId() == recordA.ID {
			t.Fatal("copied record reused the source record id")
		}
		copyByTitle[record.GetCellsMap()[copyFields["标题"]].GetStringValue()] = record
	}
	for title, quantity := range map[string]int64{"盘点A": 3, "盘点B": 9} {
		record, ok := copyByTitle[title]
		if !ok {
			t.Fatalf("copied records miss title %q: %+v", title, copyRecords)
		}
		if got := record.GetCellsMap()[copyFields["数量"]]; !proto.Equal(got, numberCellValue(quantity)) {
			t.Fatalf("record %q 数量 = %v, want %d", title, got, quantity)
		}
		wantStage := duplicateOptionTodoID
		if title == "盘点B" {
			wantStage = duplicateOptionDoneID
		}
		if got := record.GetCellsMap()[copyFields["阶段"]].GetStringValue(); got != wantStage {
			t.Fatalf("record %q 阶段 = %q, want %q", title, got, wantStage)
		}
		if record.GetVersion() != 1 {
			t.Fatalf("record %q version = %d, want 1 (new-record semantics)", title, record.GetVersion())
		}
		if record.GetCreatedBy() != duplicateOwnerID {
			t.Fatalf("record %q created_by = %q, want duplicating operator %q", title, record.GetCreatedBy(), duplicateOwnerID)
		}
		if record.GetCreatedAt() != harness.now.UnixMilli() {
			t.Fatalf("record %q created_at = %d, want %d", title, record.GetCreatedAt(), harness.now.UnixMilli())
		}
	}

	// 评论不复制：副本评论为 0，源评论保持不变。
	if counts := duplicateUnsolvedCommentCounts(t, harness, duplicateBaseID, copyTable.GetId()); len(counts) != 0 {
		t.Fatalf("copied table comment counts = %v, want empty", counts)
	}
	sourceCounts := duplicateUnsolvedCommentCounts(t, harness, duplicateBaseID, sourceTableID)
	if sourceCounts[recordA.ID] != 1 {
		t.Fatalf("source comment counts = %v, want 1 for %s", sourceCounts, recordA.ID)
	}
}

// TestDuplicateTableRemapsSelfRelationCellsToCopies 自关系值改写：关系字段指向
// 本表时，副本记录的关系单元格改写到副本记录 id，读回关系渲染与源一致。
func TestDuplicateTableRemapsSelfRelationCellsToCopies(t *testing.T) {
	harness := newDuplicateTableHarness(t, nil)
	ctx := authenticatedContext(t, duplicateOwnerID, duplicateEnterpriseID)
	sourceTable, err := harness.client.CreateTable(ctx, &structurev1.CreateTableRequest{
		BaseId: duplicateBaseID,
		Name:   "任务清单t04",
	})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	sourceTableID := sourceTable.GetTable().GetId()
	if _, err := harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: sourceTableID,
		Name:    "前置任务",
		Type:    structurev1.FieldType_FIELD_TYPE_RELATION,
		ConfigMap: configMap(t, map[string]any{
			"target_table_id": sourceTableID,
			"allow_multiple":  false,
		}),
	}); err != nil {
		t.Fatalf("CreateField(前置任务) error = %v", err)
	}
	sourceFields := duplicateFieldIDsByName(t, harness, sourceTableID)
	past := harness.now.Add(-time.Hour)
	taskB := seedDuplicateRecord(t, harness, sourceTableID, duplicateOtherID, past, 3, map[string]*commonv1.Value{
		sourceFields["标题"]: textCellValue("任务B"),
	})
	seedDuplicateRecord(t, harness, sourceTableID, duplicateOtherID, past, 2, map[string]*commonv1.Value{
		sourceFields["标题"]:    textCellValue("任务A"),
		sourceFields["前置任务"]: textCellValue(taskB.ID),
	})

	duplicated, err := harness.client.DuplicateTable(ctx, &structurev1.DuplicateTableRequest{
		TableId: sourceTableID,
	})
	if err != nil {
		t.Fatalf("DuplicateTable() error = %v", err)
	}
	copyTableID := duplicated.GetTable().GetId()
	copyFields := duplicateFieldIDsByName(t, harness, copyTableID)
	copyRecords := duplicateRecords(t, harness, duplicateBaseID, copyTableID)
	if len(copyRecords) != 2 {
		t.Fatalf("copied records = %d, want 2", len(copyRecords))
	}
	byTitle := make(map[string]*recordsv1.Record, len(copyRecords))
	for _, record := range copyRecords {
		byTitle[record.GetCellsMap()[copyFields["标题"]].GetStringValue()] = record
	}
	copyA, ok := byTitle["任务A"]
	if !ok {
		t.Fatalf("copied records miss 任务A: %+v", copyRecords)
	}
	copyB, ok := byTitle["任务B"]
	if !ok {
		t.Fatalf("copied records miss 任务B: %+v", copyRecords)
	}
	if got := copyA.GetCellsMap()[copyFields["前置任务"]].GetStringValue(); got != copyB.GetId() {
		t.Fatalf("copied self-relation cell = %q, want copied record %q", got, copyB.GetId())
	}
}

// copyFieldByName 从字段清单读回单个字段（按 name→id 投影定位）。
func copyFieldByName(
	t *testing.T,
	ids map[string]string,
	name, tableID string,
	harness *duplicateTableHarness,
) *structurev1.Field {
	t.Helper()
	fieldID, ok := ids[name]
	if !ok {
		t.Fatalf("field %q not copied to table %s", name, tableID)
	}
	response, err := harness.client.ListFields(
		authenticatedContext(t, duplicateOwnerID, duplicateEnterpriseID),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields(copy) error = %v", err)
	}
	for _, field := range response.GetFieldList() {
		if field.GetId() == fieldID {
			return field
		}
	}
	t.Fatalf("field %q (%s) not found in table %s", name, fieldID, tableID)
	return nil
}

func TestDuplicateTableRollsBackWhenRecordCopyFails(t *testing.T) {
	harness := newDuplicateTableHarness(t, &fakeTableRecordsImpact{
		err: errors.New("records copy unavailable"),
	})
	ctx := authenticatedContext(t, duplicateOwnerID, duplicateEnterpriseID)
	source, err := harness.client.CreateTable(ctx, &structurev1.CreateTableRequest{
		BaseId: duplicateBaseID,
		Name:   "回滚清单t04",
	})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}

	if _, err := harness.client.DuplicateTable(ctx, &structurev1.DuplicateTableRequest{
		TableId: source.GetTable().GetId(),
	}); err == nil {
		t.Fatal("DuplicateTable() error = nil, want records copy failure")
	}
	if len(harness.records.calls) != 1 {
		t.Fatalf("record copy calls = %d, want 1", len(harness.records.calls))
	}

	tables, err := harness.client.ListTables(ctx, &structurev1.ListTablesRequest{
		BaseId: duplicateBaseID,
	})
	if err != nil {
		t.Fatalf("ListTables() error = %v", err)
	}
	if len(tables.GetTableList()) != 1 || tables.GetTableList()[0].GetId() != source.GetTable().GetId() {
		t.Fatalf("tables after failed duplicate = %+v, want only source", tables.GetTableList())
	}
	for _, table := range tables.GetTableList() {
		if strings.Contains(table.GetName(), "副本") {
			t.Fatalf("half-copied table %q survived the failed duplicate", table.GetName())
		}
	}
}
