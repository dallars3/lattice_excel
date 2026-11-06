package structure_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/records"
	"excel_core_service/internal/structure"
)

// t06DeleteCreator 是 t06 删除级联夹具专用的记录创建人（26 位）：
// 记录/历史清理按该创建人命名空间隔离，不触碰其他套件的数据。
const t06DeleteCreator = "01J000000000000000000T06A"

var t06RecordSequence atomic.Int64

// deleteFieldHarness 在同一 bufconn 上装配 structure + records 两个服务，
// 并把真实 records.FieldConversionAdapter 注入 structure 影响面 seam，
// 使影响面统计与级联断言全部落在真实记录与视图数据上。
type deleteFieldHarness struct {
	structureClient structurev1.StructureServiceClient
	recordsClient   recordsv1.RecordsServiceClient
	db              *sql.DB
	structureRepo   *base.StructureRepository
	tableIDs        []string
}

func newDeleteFieldHarness(t *testing.T) *deleteFieldHarness {
	t.Helper()
	db, err := base.OpenDB(testMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetT06DeleteRecordsByCreator(t, db, t06DeleteCreator)
	// 清理只针对本夹具自建的表，避免全量重置影响共享库上的并行套件。
	tableIDs := &[]string{}
	t.Cleanup(func() { resetT06DeleteStructure(t, db, *tableIDs) })
	t.Cleanup(func() { resetT06DeleteRecordsByTables(t, db, *tableIDs) })

	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	roles := &fakeRoleProvider{roles: map[string]iamv1.MemberRole{
		testOwner:  iamv1.MemberRole_MEMBER_ROLE_OWNER,
		testMember: iamv1.MemberRole_MEMBER_ROLE_MEMBER,
	}}
	structureRepo := base.NewStructureRepository(db)
	recordsRepo := base.NewRecordsRepository(db)
	conversion := records.NewFieldConversionAdapter(recordsRepo, structureRepo)
	structureService := structure.NewService(
		structureRepo,
		roles,
		conversion,
		nil,
		func() time.Time { return now },
		&fakeReadAccess{},
	)
	recordsService := records.NewService(
		recordsRepo,
		// 夹具主体为所属企业内部成员，Base 级授权 seam 不会被触发（外部
		// 协作者才走该裁决），传 nil 保持 fail-closed 口径即可。
		records.NewStructureViewAccessor(structureRepo, nil),
		nil,
		nil, // 夹具不读历史（历史可见性矩阵由 records 包锁定）。
		nil,
		func() time.Time { return now },
		nil,
		records.WriterOptions{
			Outbox: bus.NewOutbox(base.NewBusStore(db)),
			Bases:  records.NewStructureBaseContextProvider(structureRepo, nil),
		},
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

	return &deleteFieldHarness{
		structureClient: structurev1.NewStructureServiceClient(conn),
		recordsClient:   recordsv1.NewRecordsServiceClient(conn),
		db:              db,
		structureRepo:   structureRepo,
		tableIDs:        *tableIDs,
	}
}

// resetT06DeleteStructure 只删除本夹具创建的数据表及其投影，保持共享库上
// 其他套件的数据不受影响。
func resetT06DeleteStructure(t *testing.T, db *sql.DB, tableIDs []string) {
	t.Helper()
	for _, tableID := range tableIDs {
		for _, statement := range []string{
			"DELETE FROM excel_structure_field_options WHERE field_id IN " +
				"(SELECT id FROM excel_structure_fields WHERE table_id = ?)",
			"DELETE FROM excel_structure_fields WHERE table_id = ?",
			"DELETE FROM excel_structure_views WHERE table_id = ?",
			"DELETE FROM excel_structure_tables WHERE id = ?",
		} {
			if _, err := db.Exec(statement, tableID); err != nil {
				t.Fatalf("reset t06 delete structure for table %s: %v", tableID, err)
			}
		}
	}
}

func resetT06DeleteRecordsByCreator(t *testing.T, db *sql.DB, creator string) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_histories WHERE record_id IN " +
			"(SELECT id FROM excel_records WHERE created_by = ?)",
		"DELETE FROM excel_records WHERE created_by = ?",
	} {
		if _, err := db.Exec(statement, creator); err != nil {
			t.Fatalf("reset t06 delete records: %v", err)
		}
	}
}

func resetT06DeleteRecordsByTables(t *testing.T, db *sql.DB, tableIDs []string) {
	t.Helper()
	for _, tableID := range tableIDs {
		for _, statement := range []string{
			"DELETE FROM excel_records_histories WHERE record_id IN " +
				"(SELECT id FROM excel_records WHERE table_id = ?)",
			"DELETE FROM excel_records WHERE table_id = ?",
		} {
			if _, err := db.Exec(statement, tableID); err != nil {
				t.Fatalf("reset t06 delete records for table %s: %v", tableID, err)
			}
		}
	}
}

func (harness *deleteFieldHarness) createTable(t *testing.T, name string) *structurev1.Table {
	t.Helper()
	response, err := harness.structureClient.CreateTable(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateTableRequest{BaseId: testBaseID, Name: name},
	)
	if err != nil {
		t.Fatalf("CreateTable(%q) error = %v", name, err)
	}
	harness.tableIDs = append(harness.tableIDs, response.GetTable().GetId())
	return response.GetTable()
}

func (harness *deleteFieldHarness) createField(
	t *testing.T,
	tableID, name string,
	fieldType structurev1.FieldType,
) *structurev1.Field {
	t.Helper()
	response, err := harness.structureClient.CreateField(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateFieldRequest{
			TableId: tableID,
			Name:    name,
			Type:    fieldType,
			ConfigMap: configMap(t, map[string]any{}),
		},
	)
	if err != nil {
		t.Fatalf("CreateField(%q) error = %v", name, err)
	}
	return response.GetField()
}

func (harness *deleteFieldHarness) createView(t *testing.T, tableID, name string) *structurev1.View {
	t.Helper()
	response, err := harness.structureClient.CreateView(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateViewRequest{TableId: tableID, Name: name, Type: "table"},
	)
	if err != nil {
		t.Fatalf("CreateView(%q) error = %v", name, err)
	}
	return response.GetView()
}

func (harness *deleteFieldHarness) updateViewConfig(
	t *testing.T,
	viewID string,
	config *structurev1.ViewConfig,
) {
	t.Helper()
	if _, err := harness.structureClient.UpdateViewConfig(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.UpdateViewConfigRequest{ViewId: viewID, ViewConfig: config},
	); err != nil {
		t.Fatalf("UpdateViewConfig(%s) error = %v", viewID, err)
	}
}

func (harness *deleteFieldHarness) viewConfig(
	t *testing.T,
	viewID string,
) *structurev1.ViewConfig {
	t.Helper()
	response, err := harness.structureClient.GetViewConfig(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.GetViewConfigRequest{ViewId: viewID},
	)
	if err != nil {
		t.Fatalf("GetViewConfig(%s) error = %v", viewID, err)
	}
	return response.GetViewConfig()
}

// seedRecord 直接落库一条记录作为夹具（断言仍走 RPC 读回）。
func (harness *deleteFieldHarness) seedRecord(
	t *testing.T,
	tableID string,
	cells map[string]*commonv1.Value,
	version int64,
	softDeleted bool,
) string {
	t.Helper()
	recordID := fmt.Sprintf("06t06%021x", t06RecordSequence.Add(1))
	payload, err := base.EncodeRecordCells(cells)
	if err != nil {
		t.Fatalf("base.EncodeRecordCells() error = %v", err)
	}
	var deletedAt any
	if softDeleted {
		deletedAt = version
	}
	if _, err := harness.db.Exec(`
		INSERT INTO excel_records (
			id, table_id, cells_json, created_by, created_at, version, soft_deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, recordID, tableID, payload, t06DeleteCreator, version, version, deletedAt); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	return recordID
}

func (harness *deleteFieldHarness) deleteField(
	t *testing.T,
	fieldID string,
	confirm bool,
) (*structurev1.DeleteFieldResponse, error) {
	t.Helper()
	return harness.structureClient.DeleteField(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.DeleteFieldRequest{FieldId: fieldID, Confirm: confirm},
	)
}

// fieldViewConfig 构造一个在全部六类槽位上都引用目标字段的视图配置。
func fieldViewConfig(fieldID string) *structurev1.ViewConfig {
	return &structurev1.ViewConfig{
		FilterList: []*structurev1.ViewFilter{{
			FieldId: fieldID,
			Op:      structurev1.ViewFilterOp_VIEW_FILTER_OP_CONTAINS,
			Value:   textValue("命中"),
		}},
		HiddenFieldList: []string{fieldID},
		FrozenFieldList: []string{fieldID},
		SortList: []*structurev1.ViewSort{{
			FieldId: fieldID,
			Dir:     structurev1.SortDir_SORT_DIR_ASC,
		}},
		GroupByList: []*structurev1.ViewGroup{{
			FieldId: fieldID,
		}},
		FieldWidthMap: map[string]int32{fieldID: 180},
	}
}

// viewConfigMentionsField 断言视图配置在任一槽位（筛选/隐藏/冻结/排序/分组/宽度）
// 是否仍引用该字段。
func viewConfigMentionsField(config *structurev1.ViewConfig, fieldID string) bool {
	if config == nil {
		return false
	}
	for _, filter := range config.GetFilterList() {
		if filter.GetFieldId() == fieldID {
			return true
		}
	}
	for _, id := range config.GetHiddenFieldList() {
		if id == fieldID {
			return true
		}
	}
	for _, id := range config.GetFrozenFieldList() {
		if id == fieldID {
			return true
		}
	}
	for _, sort := range config.GetSortList() {
		if sort.GetFieldId() == fieldID {
			return true
		}
	}
	for _, group := range config.GetGroupByList() {
		if group.GetFieldId() == fieldID {
			return true
		}
	}
	_, width := config.GetFieldWidthMap()[fieldID]
	return width
}

// TestDeleteFieldConfirmCleansEveryViewReference 覆盖工单 06 的两条验收：
// confirm=false 只返回影响面不动任何数据；confirm=true 删除字段并把每个视图
// 配置中的筛选/隐藏/冻结/排序/分组/宽度引用一并清掉，删除后字段不可达 4002。
func TestDeleteFieldConfirmCleansEveryViewReference(t *testing.T) {
	harness := newDeleteFieldHarness(t)
	table := harness.createTable(t, "t06删除级联")
	target := harness.createField(
		t,
		table.GetId(),
		"待删字段",
		structurev1.FieldType_FIELD_TYPE_TEXT,
	)

	first := harness.seedRecord(
		t, table.GetId(),
		map[string]*commonv1.Value{target.GetId(): textValue("第一行")},
		1, false,
	)
	harness.seedRecord(
		t, table.GetId(),
		map[string]*commonv1.Value{target.GetId(): textValue("第二行")},
		1, false,
	)
	harness.seedRecord(
		t, table.GetId(),
		map[string]*commonv1.Value{target.GetId(): textValue("已删行")},
		1, true,
	)

	views := mustListViewsFor(t, harness, table.GetId())
	if len(views) != 1 {
		t.Fatalf("default view count = %d, want 1", len(views))
	}
	extraView := harness.createView(t, table.GetId(), "t06视图二")
	for _, view := range []*structurev1.View{views[0], extraView} {
		harness.updateViewConfig(t, view.GetId(), fieldViewConfig(target.GetId()))
	}

	preview, err := harness.deleteField(t, target.GetId(), false)
	if err != nil {
		t.Fatalf("DeleteField(preview) error = %v", err)
	}
	assertDeleteImpact(t, preview, 2, 0, 0)
	harness.assertFieldExists(t, table.GetId(), target.GetId(), true)
	for _, view := range []*structurev1.View{views[0], extraView} {
		if !viewConfigMentionsField(harness.viewConfig(t, view.GetId()), target.GetId()) {
			t.Fatalf("view %s lost field reference during preview", view.GetId())
		}
	}

	confirmed, err := harness.deleteField(t, target.GetId(), true)
	if err != nil {
		t.Fatalf("DeleteField(confirm) error = %v", err)
	}
	assertDeleteImpact(t, confirmed, 2, 0, 0)
	harness.assertFieldExists(t, table.GetId(), target.GetId(), false)
	for _, view := range []*structurev1.View{views[0], extraView} {
		if viewConfigMentionsField(harness.viewConfig(t, view.GetId()), target.GetId()) {
			t.Fatalf("view %s still references deleted field %s", view.GetId(), target.GetId())
		}
	}

	if _, err := harness.deleteField(t, target.GetId(), true); err == nil {
		t.Fatal("second DeleteField() error = nil, want 4002")
	} else {
		requireErrorCode(t, err, 4002)
	}

	// 记录引用已删字段：写单元格返回契约错误（1001 字段不在表结构中）。
	if _, err := harness.recordsClient.UpdateCell(
		recordsReadContext(t),
		&recordsv1.UpdateCellRequest{
			RecordId: first,
			FieldId:  target.GetId(),
			Value:    textValue("又写"),
		},
	); err == nil {
		t.Fatal("UpdateCell(deleted field) error = nil, want contract error")
	} else {
		requireErrorCode(t, err, 1001)
	}
}

// TestDeleteFieldPreviewLeavesViewConfigUntouched 锁定 confirm=false 的只读
// 语义：预览不触发任何删除，视图配置原样保留。
func TestDeleteFieldPreviewLeavesViewConfigUntouched(t *testing.T) {
	harness := newDeleteFieldHarness(t)
	table := harness.createTable(t, "t06预览不动数据")
	target := harness.createField(
		t,
		table.GetId(),
		"预览字段",
		structurev1.FieldType_FIELD_TYPE_TEXT,
	)
	harness.seedRecord(
		t, table.GetId(),
		map[string]*commonv1.Value{target.GetId(): textValue("值")},
		1, false,
	)
	views := mustListViewsFor(t, harness, table.GetId())
	harness.updateViewConfig(t, views[0].GetId(), fieldViewConfig(target.GetId()))

	if _, err := harness.deleteField(t, target.GetId(), false); err != nil {
		t.Fatalf("DeleteField(preview) error = %v", err)
	}
	harness.assertFieldExists(t, table.GetId(), target.GetId(), true)
	if !viewConfigMentionsField(harness.viewConfig(t, views[0].GetId()), target.GetId()) {
		t.Fatal("preview must not touch view config")
	}
}

func (harness *deleteFieldHarness) assertFieldExists(
	t *testing.T,
	tableID, fieldID string,
	want bool,
) {
	t.Helper()
	response, err := harness.structureClient.ListFields(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	found := false
	for _, field := range response.GetFieldList() {
		if field.GetId() == fieldID {
			found = true
			break
		}
	}
	if found != want {
		t.Fatalf("field %s exists = %v, want %v", fieldID, found, want)
	}
}

func mustListViewsFor(
	t *testing.T,
	harness *deleteFieldHarness,
	tableID string,
) []*structurev1.View {
	t.Helper()
	response, err := harness.structureClient.ListViews(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListViewsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListViews() error = %v", err)
	}
	return response.GetViewList()
}
