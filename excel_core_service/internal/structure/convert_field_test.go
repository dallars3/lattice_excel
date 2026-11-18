package structure_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	iamv1 "excel/api/iam/v1"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/records"
	"excel_core_service/internal/structure"
)

// t05ConvertCreator 是 t05 转换夹具专用的记录创建人（26 位）：
// 记录/历史清理按该创建人命名空间隔离，不触碰其他套件的数据。
const t05ConvertCreator = "01J000000000000000000T05A"

var t05RecordSequence atomic.Int64

// convertHarness 在同一 bufconn 上装配 structure + records 两个服务，
// 并把真实 records.FieldConversionAdapter 注入 structure 影响面 seam，
// 使预览/确认断言全部落在真实记录数据上。
type convertHarness struct {
	structureClient structurev1.StructureServiceClient
	recordsClient   recordsv1.RecordsServiceClient
	db              *sql.DB
	structureRepo   *base.StructureRepository
	tableIDs        []string
}

func newConvertHarness(t *testing.T) *convertHarness {
	t.Helper()
	db := testmysql.OpenTestDB(t, testMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetStructureData(t, db)
	resetT05ConvertRecordsByCreator(t, db, t05ConvertCreator)
	// 清理只针对本夹具自建的表，避免全量重置影响共享库上的并行套件。
	tableIDs := &[]string{}
	t.Cleanup(func() { resetT05ConvertStructure(t, db, *tableIDs) })
	t.Cleanup(func() { resetT05ConvertRecordsByTables(t, db, *tableIDs) })

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
		// 缺陷 #18：结构写与广播事件同事务落 outbox，未装配 fail-closed 9001。
		structure.ServiceOptions{Outbox: bus.NewOutbox(base.NewBusStore(db))},
	)
	recordsService := records.NewService(
		recordsRepo,
		// 夹具主体均为所属企业内部成员，Base 级授权 seam 不会被触发（外部
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

	harness := &convertHarness{
		structureClient: structurev1.NewStructureServiceClient(conn),
		recordsClient:   recordsv1.NewRecordsServiceClient(conn),
		db:              db,
		structureRepo:   structureRepo,
		tableIDs:        *tableIDs,
	}
	return harness
}

// resetT05ConvertStructure 只删除本夹具创建的数据表及其投影，保持共享库上
// 其他套件的数据不受影响。
func resetT05ConvertStructure(t *testing.T, db *sql.DB, tableIDs []string) {
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
				t.Fatalf("reset t05 convert structure for table %s: %v", tableID, err)
			}
		}
	}
}

func resetT05ConvertRecordsByCreator(t *testing.T, db *sql.DB, creator string) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_records_histories WHERE record_id IN " +
			"(SELECT id FROM excel_records WHERE created_by = ?)",
		"DELETE FROM excel_records WHERE created_by = ?",
	} {
		if _, err := db.Exec(statement, creator); err != nil {
			t.Fatalf("reset t05 convert records: %v", err)
		}
	}
}

func resetT05ConvertRecordsByTables(t *testing.T, db *sql.DB, tableIDs []string) {
	t.Helper()
	for _, tableID := range tableIDs {
		for _, statement := range []string{
			"DELETE FROM excel_records_histories WHERE record_id IN " +
				"(SELECT id FROM excel_records WHERE table_id = ?)",
			"DELETE FROM excel_records WHERE table_id = ?",
		} {
			if _, err := db.Exec(statement, tableID); err != nil {
				t.Fatalf("reset t05 convert records for table %s: %v", tableID, err)
			}
		}
	}
}

func (harness *convertHarness) createTable(t *testing.T, name string) *structurev1.Table {
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

// createConvertField 经 RPC 建字段（与 createField 同口径，但挂在转换夹具上）。
func createConvertField(
	t *testing.T,
	harness *convertHarness,
	tableID, name string,
	fieldType structurev1.FieldType,
	config map[string]any,
) *structurev1.Field {
	t.Helper()
	response, err := harness.structureClient.CreateField(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateFieldRequest{
			TableId:   tableID,
			Name:      name,
			Type:      fieldType,
			ConfigMap: configMap(t, config),
		},
	)
	if err != nil {
		t.Fatalf("CreateField(%q) error = %v", name, err)
	}
	return response.GetField()
}

// seedRecord 直接落库一条记录作为夹具（断言仍走 RPC 读回）。
func (harness *convertHarness) seedRecord(
	t *testing.T,
	tableID string,
	cells map[string]*commonv1.Value,
	version int64,
	softDeleted bool,
) string {
	t.Helper()
	recordID := fmt.Sprintf("05t05%021x", t05RecordSequence.Add(1))
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
	`, recordID, tableID, payload, t05ConvertCreator, version, version, deletedAt); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	return recordID
}

// recordState 通过 GetRecord RPC 读回单元格与版本（外部行为断言口）。
func (harness *convertHarness) recordState(
	t *testing.T,
	recordID string,
) (map[string]*commonv1.Value, int64) {
	t.Helper()
	response, err := harness.recordsClient.GetRecord(
		recordsReadContext(t),
		&recordsv1.GetRecordRequest{RecordId: recordID, BaseId: testBaseID},
	)
	if err != nil {
		t.Fatalf("GetRecord(%s) error = %v", recordID, err)
	}
	return response.GetRecord().GetCellsMap(), response.GetRecord().GetVersion()
}

// persistedField 读回单个字段行（类型/配置断言口）。
func (harness *convertHarness) persistedField(
	t *testing.T,
	tableID, fieldID string,
) *structurev1.Field {
	t.Helper()
	response, err := harness.structureClient.ListFields(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	for _, field := range response.GetFieldList() {
		if field.GetId() == fieldID {
			return field
		}
	}
	t.Fatalf("field %s not found", fieldID)
	return nil
}

func recordsReadContext(t *testing.T) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       testOwner,
		EnterpriseId: testEnterprise,
		Internal:     true,
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
		"x-row-scope",
		"ROW_SCOPE_ALL",
	)
}

func textValue(text string) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: text}}
}

func listValue(ids ...string) *commonv1.Value {
	items := make([]*commonv1.Value, 0, len(ids))
	for _, id := range ids {
		items = append(items, textValue(id))
	}
	return &commonv1.Value{
		Kind: &commonv1.Value_ListValue{ListValue: &commonv1.ValueList{ValueList: items}},
	}
}

func TestConvertFieldPreviewCountsRealRecordsWithoutWriting(t *testing.T) {
	harness := newConvertHarness(t)
	table := harness.createTable(t, "t05转换预览")
	field := createConvertField(t, harness, table.GetId(),
		"金额文本", structurev1.FieldType_FIELD_TYPE_TEXT, map[string]any{})
	numeric := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("123")}, 3, false)
	harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("abc")}, 3, false)
	harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{}, 3, false)
	harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("456")}, 3, true)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	preview, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    field.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_NUMBER,
		Confirm:    false,
	})
	if err != nil {
		t.Fatalf("ConvertField(preview) error = %v", err)
	}
	if preview.GetAffectedCount() != 2 || preview.GetClearedCount() != 1 {
		t.Fatalf("preview counts = (%d,%d), want (2,1)",
			preview.GetAffectedCount(), preview.GetClearedCount())
	}
	if got := harness.persistedField(t, table.GetId(), field.GetId()).GetType(); got != structurev1.FieldType_FIELD_TYPE_TEXT {
		t.Fatalf("preview changed field type to %s", got)
	}
	cells, _ := harness.recordState(t, numeric)
	value, exists := cells[field.GetId()]
	if !exists || value.GetStringValue() != "123" {
		t.Fatalf("preview mutated cell value: %v", value)
	}
}

func TestConvertFieldConfirmMigratesTextToNumberAndClearsUnparsable(t *testing.T) {
	harness := newConvertHarness(t)
	table := harness.createTable(t, "t05转数字")
	field := createConvertField(t, harness, table.GetId(),
		"金额文本", structurev1.FieldType_FIELD_TYPE_TEXT, map[string]any{})
	numeric := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("123")}, 3, false)
	decimal := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("1,234.5")}, 3, false)
	dirty := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("abc")}, 3, false)
	empty := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{}, 3, false)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	confirmed, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    field.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_NUMBER,
		Confirm:    true,
	})
	if err != nil {
		t.Fatalf("ConvertField(confirm) error = %v", err)
	}
	if confirmed.GetField().GetType() != structurev1.FieldType_FIELD_TYPE_NUMBER {
		t.Fatalf("confirmed field type = %s, want NUMBER", confirmed.GetField().GetType())
	}
	if confirmed.GetAffectedCount() != 3 || confirmed.GetClearedCount() != 1 {
		t.Fatalf("confirmed counts = (%d,%d), want (3,1)",
			confirmed.GetAffectedCount(), confirmed.GetClearedCount())
	}
	if got := harness.persistedField(t, table.GetId(), field.GetId()).GetType(); got != structurev1.FieldType_FIELD_TYPE_NUMBER {
		t.Fatalf("persisted field type = %s, want NUMBER", got)
	}
	cells, version := harness.recordState(t, numeric)
	if got := cells[field.GetId()].GetIntValue(); got != 123 {
		t.Fatalf("numeric cell = %v, want int 123", cells[field.GetId()])
	}
	if version != 4 {
		t.Fatalf("converted record version = %d, want 4", version)
	}
	cells, _ = harness.recordState(t, decimal)
	if got := cells[field.GetId()].GetDoubleValue(); math.Abs(got-1234.5) > 1e-9 {
		t.Fatalf("thousands cell = %v, want 1234.5", cells[field.GetId()])
	}
	cells, version = harness.recordState(t, dirty)
	if _, exists := cells[field.GetId()]; exists {
		t.Fatalf("dirty cell not cleared: %v", cells[field.GetId()])
	}
	if version != 4 {
		t.Fatalf("cleared record version = %d, want 4", version)
	}
	_, version = harness.recordState(t, empty)
	if version != 3 {
		t.Fatalf("empty record version = %d, want untouched 3", version)
	}
}

func TestConvertFieldConfirmParsesTextToDateAndClearsInvalid(t *testing.T) {
	harness := newConvertHarness(t)
	table := harness.createTable(t, "t05转日期")
	field := createConvertField(t, harness, table.GetId(),
		"截止文本", structurev1.FieldType_FIELD_TYPE_TEXT, map[string]any{})
	valid := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("2026-11-02")}, 3, false)
	slashed := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("2026/11/03")}, 3, false)
	chinese := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("2026年11月4日")}, 3, false)
	invalid := harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{field.GetId(): textValue("明天")}, 3, false)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	confirmed, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    field.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_DATE,
		ConfigMap:  configMap(t, map[string]any{"format": "YYYY-MM-DD"}),
		Confirm:    true,
	})
	if err != nil {
		t.Fatalf("ConvertField(confirm) error = %v", err)
	}
	if confirmed.GetAffectedCount() != 4 || confirmed.GetClearedCount() != 1 {
		t.Fatalf("confirmed counts = (%d,%d), want (4,1)",
			confirmed.GetAffectedCount(), confirmed.GetClearedCount())
	}
	want := map[string]int64{
		valid:   time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC).UnixMilli(),
		slashed: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC).UnixMilli(),
		chinese: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}
	for recordID, millis := range want {
		cells, _ := harness.recordState(t, recordID)
		if got := cells[field.GetId()].GetDateValueMs(); got != millis {
			t.Fatalf("date cell for %s = %d, want %d", recordID, got, millis)
		}
	}
	cells, _ := harness.recordState(t, invalid)
	if _, exists := cells[field.GetId()]; exists {
		t.Fatalf("invalid date cell not cleared: %v", cells[field.GetId()])
	}
}

func TestConvertFieldSelectTargetsPreserveOptionsAndMatchByName(t *testing.T) {
	harness := newConvertHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	// 单选 → 多选：不传 config 时选项原样继承（id 稳定），单值变单元素。
	selectTable := harness.createTable(t, "t05选项")
	selectField := createConvertField(t, harness, selectTable.GetId(),
		"优先级", structurev1.FieldType_FIELD_TYPE_SELECT, map[string]any{
			"option_list": []any{
				map[string]any{"id": "05t05opt-a", "name": "高", "color": ""},
				map[string]any{"id": "05t05opt-b", "name": "中", "color": ""},
			},
		})
	kept := harness.seedRecord(t, selectTable.GetId(), map[string]*commonv1.Value{selectField.GetId(): textValue("05t05opt-a")}, 3, false)
	preview, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    selectField.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_MULTISELECT,
	})
	if err != nil {
		t.Fatalf("select→multiselect preview error = %v", err)
	}
	if preview.GetAffectedCount() != 1 || preview.GetClearedCount() != 0 {
		t.Fatalf("select→multiselect preview counts = (%d,%d), want (1,0)",
			preview.GetAffectedCount(), preview.GetClearedCount())
	}
	if _, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    selectField.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_MULTISELECT,
		Confirm:    true,
	}); err != nil {
		t.Fatalf("select→multiselect confirm error = %v", err)
	}
	field := harness.persistedField(t, selectTable.GetId(), selectField.GetId())
	if field.GetType() != structurev1.FieldType_FIELD_TYPE_MULTISELECT {
		t.Fatalf("field type = %s, want MULTISELECT", field.GetType())
	}
	options := field.GetConfigMap().GetFields()["option_list"].GetListValue().GetValues()
	if len(options) != 2 || options[0].GetStructValue().GetFields()["id"].GetStringValue() != "05t05opt-a" {
		t.Fatalf("converted options lost source ids: %v", options)
	}
	cells, _ := harness.recordState(t, kept)
	items := cells[selectField.GetId()].GetListValue().GetValueList()
	if len(items) != 1 || items[0].GetStringValue() != "05t05opt-a" {
		t.Fatalf("select→multiselect cell = %v, want [05t05opt-a]", items)
	}

	// 文本 → 单选：按选项名精确匹配，匹配不到置空且不自动新建选项。
	textTable := harness.createTable(t, "t05文本转选项")
	textField := createConvertField(t, harness, textTable.GetId(),
		"等级文本", structurev1.FieldType_FIELD_TYPE_TEXT, map[string]any{})
	matched := harness.seedRecord(t, textTable.GetId(), map[string]*commonv1.Value{textField.GetId(): textValue("高")}, 3, false)
	unmatched := harness.seedRecord(t, textTable.GetId(), map[string]*commonv1.Value{textField.GetId(): textValue("不存在")}, 3, false)
	confirmed, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    textField.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_SELECT,
		ConfigMap: configMap(t, map[string]any{
			"option_list": []any{
				map[string]any{"id": "05t05opt-a", "name": "高", "color": ""},
				map[string]any{"id": "05t05opt-b", "name": "中", "color": ""},
			},
		}),
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("text→select confirm error = %v", err)
	}
	if confirmed.GetAffectedCount() != 2 || confirmed.GetClearedCount() != 1 {
		t.Fatalf("text→select counts = (%d,%d), want (2,1)",
			confirmed.GetAffectedCount(), confirmed.GetClearedCount())
	}
	cells, _ = harness.recordState(t, matched)
	if got := cells[textField.GetId()].GetStringValue(); got != "05t05opt-a" {
		t.Fatalf("text→select cell = %q, want matched option id 05t05opt-a", got)
	}
	cells, _ = harness.recordState(t, unmatched)
	if _, exists := cells[textField.GetId()]; exists {
		t.Fatalf("unmatched text cell not cleared: %v", cells[textField.GetId()])
	}

	// 多选 → 单选：保留第一个选项，其余丢弃不视为清空。
	multiTable := harness.createTable(t, "t05多转单")
	multiField := createConvertField(t, harness, multiTable.GetId(),
		"标签", structurev1.FieldType_FIELD_TYPE_MULTISELECT, map[string]any{
			"option_list": []any{
				map[string]any{"id": "05t05opt-a", "name": "高", "color": ""},
				map[string]any{"id": "05t05opt-b", "name": "中", "color": ""},
			},
		})
	multiRecord := harness.seedRecord(t, multiTable.GetId(), map[string]*commonv1.Value{multiField.GetId(): listValue("05t05opt-a", "05t05opt-b")}, 3, false)
	confirmed, err = harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    multiField.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_SELECT,
		Confirm:    true,
	})
	if err != nil {
		t.Fatalf("multiselect→select confirm error = %v", err)
	}
	if confirmed.GetAffectedCount() != 1 || confirmed.GetClearedCount() != 0 {
		t.Fatalf("multiselect→select counts = (%d,%d), want (1,0)",
			confirmed.GetAffectedCount(), confirmed.GetClearedCount())
	}
	cells, _ = harness.recordState(t, multiRecord)
	if got := cells[multiField.GetId()].GetStringValue(); got != "05t05opt-a" {
		t.Fatalf("multiselect→select cell = %q, want first option 05t05opt-a", got)
	}
}

func TestConvertFieldToTextRendersDisplayValues(t *testing.T) {
	harness := newConvertHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	// 数字 → 文本：按数值输出。
	numberTable := harness.createTable(t, "t05数字转文本")
	numberField := createConvertField(t, harness, numberTable.GetId(),
		"数量", structurev1.FieldType_FIELD_TYPE_NUMBER, map[string]any{})
	intRecord := harness.seedRecord(t, numberTable.GetId(), map[string]*commonv1.Value{
		numberField.GetId(): {Kind: &commonv1.Value_IntValue{IntValue: 123}},
	}, 3, false)
	decimalRecord := harness.seedRecord(t, numberTable.GetId(), map[string]*commonv1.Value{
		numberField.GetId(): {Kind: &commonv1.Value_DoubleValue{DoubleValue: 12.5}},
	}, 3, false)
	confirmed, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    numberField.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_TEXT,
		Confirm:    true,
	})
	if err != nil {
		t.Fatalf("number→text confirm error = %v", err)
	}
	if confirmed.GetAffectedCount() != 2 || confirmed.GetClearedCount() != 0 {
		t.Fatalf("number→text counts = (%d,%d), want (2,0)",
			confirmed.GetAffectedCount(), confirmed.GetClearedCount())
	}
	cells, _ := harness.recordState(t, intRecord)
	if got := cells[numberField.GetId()].GetStringValue(); got != "123" {
		t.Fatalf("int→text cell = %q, want 123", got)
	}
	cells, _ = harness.recordState(t, decimalRecord)
	if got := cells[numberField.GetId()].GetStringValue(); got != "12.5" {
		t.Fatalf("double→text cell = %q, want 12.5", got)
	}

	// 单选 → 文本：取选项名。
	selectTable := harness.createTable(t, "t05选项转文本")
	selectField := createConvertField(t, harness, selectTable.GetId(),
		"状态", structurev1.FieldType_FIELD_TYPE_SELECT, map[string]any{
			"option_list": []any{
				map[string]any{"id": "05t05opt-a", "name": "进行中", "color": ""},
			},
		})
	selectRecord := harness.seedRecord(t, selectTable.GetId(), map[string]*commonv1.Value{selectField.GetId(): textValue("05t05opt-a")}, 3, false)
	if _, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    selectField.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_TEXT,
		Confirm:    true,
	}); err != nil {
		t.Fatalf("select→text confirm error = %v", err)
	}
	cells, _ = harness.recordState(t, selectRecord)
	if got := cells[selectField.GetId()].GetStringValue(); got != "进行中" {
		t.Fatalf("select→text cell = %q, want option name 进行中", got)
	}

	// 日期 → 文本：按 YYYY-MM-DD 输出。
	dateTable := harness.createTable(t, "t05日期转文本")
	dateField := createConvertField(t, harness, dateTable.GetId(),
		"截止", structurev1.FieldType_FIELD_TYPE_DATE, map[string]any{})
	dateRecord := harness.seedRecord(t, dateTable.GetId(), map[string]*commonv1.Value{
		dateField.GetId(): {Kind: &commonv1.Value_DateValueMs{DateValueMs: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC).UnixMilli()}},
	}, 3, false)
	if _, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    dateField.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_TEXT,
		Confirm:    true,
	}); err != nil {
		t.Fatalf("date→text confirm error = %v", err)
	}
	cells, _ = harness.recordState(t, dateRecord)
	if got := cells[dateField.GetId()].GetStringValue(); got != "2026-11-02" {
		t.Fatalf("date→text cell = %q, want 2026-11-02", got)
	}
}

func TestConvertFieldRejectionsKeepContractCodes(t *testing.T) {
	harness := newConvertHarness(t)
	table := harness.createTable(t, "t05转换拒绝")
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	formula := createConvertField(t, harness, table.GetId(),
		"公式", structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "1 + 1"})
	relation := createConvertField(t, harness, table.GetId(),
		"关联", structurev1.FieldType_FIELD_TYPE_RELATION, map[string]any{"target_table_id": table.GetId()})
	date := createConvertField(t, harness, table.GetId(),
		"日期", structurev1.FieldType_FIELD_TYPE_DATE, map[string]any{})
	text := createConvertField(t, harness, table.GetId(),
		"文本", structurev1.FieldType_FIELD_TYPE_TEXT, map[string]any{})

	rejections := []struct {
		name       string
		fieldID    string
		targetType structurev1.FieldType
		confirm    bool
		want       int
	}{
		{"公式转文本", formula.GetId(), structurev1.FieldType_FIELD_TYPE_TEXT, true, 1001},
		{"关联转引用", relation.GetId(), structurev1.FieldType_FIELD_TYPE_LOOKUP, true, 1001},
		{"文本转公式", text.GetId(), structurev1.FieldType_FIELD_TYPE_FORMULA, true, 1001},
		{"文本转关联", text.GetId(), structurev1.FieldType_FIELD_TYPE_RELATION, false, 1001},
		{"文本转引用", text.GetId(), structurev1.FieldType_FIELD_TYPE_LOOKUP, true, 1001},
		{"日期转数字", date.GetId(), structurev1.FieldType_FIELD_TYPE_NUMBER, true, 1001},
		{"目标类型缺省", text.GetId(), structurev1.FieldType_FIELD_TYPE_UNSPECIFIED, true, 1001},
	}
	for _, rejection := range rejections {
		_, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
			FieldId:    rejection.fieldID,
			TargetType: rejection.targetType,
			Confirm:    rejection.confirm,
		})
		if err == nil {
			t.Fatalf("%s: ConvertField error = nil, want %d", rejection.name, rejection.want)
		}
		requireErrorCode(t, err, rejection.want)
	}
	if got := harness.persistedField(t, table.GetId(), formula.GetId()).GetType(); got != structurev1.FieldType_FIELD_TYPE_FORMULA {
		t.Fatalf("rejected conversion changed formula field to %s", got)
	}
	if got := harness.persistedField(t, table.GetId(), date.GetId()).GetType(); got != structurev1.FieldType_FIELD_TYPE_DATE {
		t.Fatalf("rejected conversion changed date field to %s", got)
	}

	if _, err := harness.structureClient.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    "05t05-missing-field",
		TargetType: structurev1.FieldType_FIELD_TYPE_TEXT,
		Confirm:    true,
	}); err == nil {
		t.Fatalf("missing field: ConvertField error = nil, want 4002")
	} else {
		requireErrorCode(t, err, 4002)
	}

	_, err := harness.structureClient.ConvertField(
		authenticatedContext(t, testMember, testEnterprise),
		&structurev1.ConvertFieldRequest{
			FieldId:    text.GetId(),
			TargetType: structurev1.FieldType_FIELD_TYPE_NUMBER,
			Confirm:    true,
		},
	)
	requireErrorCode(t, err, 3001)
}

// TestConvertFieldRejectsPrimaryField 锁主字段保护（P0 TC-040-02）：主字段
// 类型转换预览与确认都必须拒绝——工单 05 修通转换后暴露的缺失守卫。
func TestConvertFieldRejectsPrimaryField(t *testing.T) {
	harness := newConvertHarness(t)
	table := harness.createTable(t, "t05主字段转换禁区")
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	fields, err := harness.structureClient.ListFields(ctx,
		&structurev1.ListFieldsRequest{TableId: table.GetId()})
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	var primary *structurev1.Field
	for _, field := range fields.GetFieldList() {
		if field.GetIsPrimary() {
			primary = field
			break
		}
	}
	if primary == nil {
		t.Fatal("primary field missing from default table")
	}
	for _, confirm := range []bool{false, true} {
		if _, err := harness.structureClient.ConvertField(ctx,
			&structurev1.ConvertFieldRequest{
				FieldId:    primary.GetId(),
				TargetType: structurev1.FieldType_FIELD_TYPE_NUMBER,
				Confirm:    confirm,
			}); err == nil {
			t.Fatalf("ConvertField(primary, confirm=%v) error = nil, want 1001", confirm)
		}
	}
	if got := harness.persistedField(t, table.GetId(), primary.GetId()).GetType(); got != primary.GetType() {
		t.Fatalf("primary field type changed to %s", got)
	}
}
