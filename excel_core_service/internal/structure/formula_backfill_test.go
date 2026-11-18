// structure/formula_backfill_test.go：N5 缺陷的端到端验收（票 03）。结构域
// 新建/复制/转类型公式或引用字段后，回填种子作业经 outbox 投递到 calc 消费
// 者，存量记录的列值被分批幂等物化——单记录读取返回物化值；转出/删除按
// 删除字段级联同口径清理物化列；回填与既有写链编辑互不冲突。
package structure_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	commonv1 "excel/api/common/v1"
	calcv1 "excel/api/calc/v1"
	iamv1 "excel/api/iam/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel-libs/eventbus"
	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/calc"
	"excel_core_service/internal/records"
	"excel_core_service/internal/structure"
)

// 回填夹具使用专属 base/创建人命名空间：outbox 与物化行清理按命名空间隔离，
// 不触碰同库其他套件的数据。
const (
	bfBaseID  = "01J00000000000000000014001"
	bfCreator = "01J00000000000000000014002"
)

var bfRecordSequence atomic.Int64

// backfillHarness 在同一 bufconn 上装配 structure + records + calc 三个服务
// （与 cmd/main.go 生产装配同形），并启动 outbox 转发与 calc.job 消费循环。
type backfillHarness struct {
	structureClient structurev1.StructureServiceClient
	recordsClient   recordsv1.RecordsServiceClient
	calcClient      calcv1.CalcServiceClient
	db              *sql.DB
	forwarder       *bus.Forwarder
	consumer        *bus.Consumer
	tableIDs        *[]string
}

func newBackfillHarness(t *testing.T) *backfillHarness {
	t.Helper()
	db := testmysql.OpenTestDB(t, testMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	tableIDs := &[]string{}
	t.Cleanup(func() { resetBackfillFixture(t, db, *tableIDs) })

	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	roles := &fakeRoleProvider{roles: map[string]iamv1.MemberRole{
		testOwner:  iamv1.MemberRole_MEMBER_ROLE_OWNER,
		testMember: iamv1.MemberRole_MEMBER_ROLE_MEMBER,
	}}
	structureRepo := base.NewStructureRepository(db)
	recordsRepo := base.NewRecordsRepository(db)
	calcRepo := base.NewCalcRepository(db)
	busStore := base.NewBusStore(db)
	outbox := bus.NewOutbox(busStore)

	structureService := structure.NewService(
		structureRepo,
		roles,
		records.NewStructureImpactAdapter(recordsRepo, structureRepo, calcRepo, time.Now),
		nil,
		func() time.Time { return now },
		&fakeReadAccess{allowed: map[string]bool{bfBaseID: true}},
		structure.ServiceOptions{
			Outbox:         outbox,
			CalcRepository: calcRepo,
		},
	)
	calcService := calc.NewService(
		calcRepo,
		structure.NewSchemaProvider(structureRepo),
		records.NewCalcRecomputeAdapter(recordsRepo, structureRepo, calcRepo),
		outbox,
		func() time.Time { return now },
	)
	recordsService := records.NewService(
		recordsRepo,
		records.NewStructureViewAccessor(structureRepo, nil),
		nil,
		nil,
		records.NewCalcFormulaValueProvider(calcRepo),
		func() time.Time { return now },
		nil,
		records.WriterOptions{
			Outbox:         outbox,
			Bases:          records.NewStructureBaseContextProvider(structureRepo, nil),
			Evaluator:      calcService,
			ComputedWriter: records.NewCalcComputedWriter(calcRepo),
		},
	)

	memoryBus := eventbus.NewMemoryBus()
	forwarder := bus.NewForwarder(busStore, memoryBus, bus.ForwarderConfig{})
	consumer := bus.NewConsumer(busStore, memoryBus, bus.ConsumerConfig{
		Retry: eventbus.RetryPolicy{
			Initial:     10 * time.Millisecond,
			Max:         50 * time.Millisecond,
			MaxAttempts: 5,
		},
	})
	if err := calc.Consumers(consumer, calcService); err != nil {
		t.Fatalf("calc.Consumers() error = %v", err)
	}
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	if err := consumer.Start(consumerCtx); err != nil {
		consumerCancel()
		t.Fatalf("consumer.Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = consumer.Stop(stopCtx)
		consumerCancel()
	})

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	structure.Register(server, structureService)
	records.Register(server, recordsService)
	calc.Register(server, calcService)
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

	return &backfillHarness{
		structureClient: structurev1.NewStructureServiceClient(conn),
		recordsClient:   recordsv1.NewRecordsServiceClient(conn),
		calcClient:      calcv1.NewCalcServiceClient(conn),
		db:              db,
		forwarder:       forwarder,
		consumer:        consumer,
		tableIDs:        tableIDs,
	}
}

// resetBackfillFixture 只清理本夹具自建表的数据：结构投影、记录行、calc 作业
// 与物化行、以及夹具命名空间（专属 base_id/创建人）下的 outbox 事件。
func resetBackfillFixture(t *testing.T, db *sql.DB, tableIDs []string) {
	t.Helper()
	for _, tableID := range tableIDs {
		for _, statement := range []string{
			"DELETE FROM excel_calc_materialized WHERE record_id IN " +
				"(SELECT id FROM excel_records WHERE table_id = ?)",
			"DELETE FROM excel_calc_jobs WHERE table_id = ?",
			"DELETE FROM excel_records_histories WHERE record_id IN " +
				"(SELECT id FROM excel_records WHERE table_id = ?)",
			"DELETE FROM excel_records WHERE table_id = ?",
			"DELETE FROM excel_structure_field_options WHERE field_id IN " +
				"(SELECT id FROM excel_structure_fields WHERE table_id = ?)",
			"DELETE FROM excel_structure_fields WHERE table_id = ?",
			"DELETE FROM excel_structure_views WHERE table_id = ?",
			"DELETE FROM excel_structure_tables WHERE id = ?",
		} {
			if _, err := db.Exec(statement, tableID); err != nil {
				t.Fatalf("reset backfill fixture for table %s: %v", tableID, err)
			}
		}
	}
	if _, err := db.Exec("DELETE FROM excel_bus_outbox WHERE base_id = ?", bfBaseID); err != nil {
		t.Fatalf("reset backfill outbox: %v", err)
	}
}

func (harness *backfillHarness) createTable(t *testing.T, name string) *structurev1.Table {
	t.Helper()
	response, err := harness.structureClient.CreateTable(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateTableRequest{BaseId: bfBaseID, Name: name},
	)
	if err != nil {
		t.Fatalf("CreateTable(%q) error = %v", name, err)
	}
	*harness.tableIDs = append(*harness.tableIDs, response.GetTable().GetId())
	return response.GetTable()
}

func (harness *backfillHarness) createField(
	t *testing.T,
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

func (harness *backfillHarness) convertField(
	t *testing.T,
	fieldID string,
	targetType structurev1.FieldType,
	config map[string]any,
	confirm bool,
) *structurev1.ConvertFieldResponse {
	t.Helper()
	response, err := harness.structureClient.ConvertField(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ConvertFieldRequest{
			FieldId:    fieldID,
			TargetType: targetType,
			ConfigMap:  configMap(t, config),
			Confirm:    confirm,
		},
	)
	if err != nil {
		t.Fatalf("ConvertField(%s -> %s, confirm=%v) error = %v", fieldID, targetType, confirm, err)
	}
	return response
}

// seedRecord 直接落库一条记录作为夹具（读取断言仍走 RPC）。
func (harness *backfillHarness) seedRecord(
	t *testing.T,
	tableID string,
	cells map[string]*commonv1.Value,
) string {
	t.Helper()
	recordID := fmt.Sprintf("01JBF00000000000000000%04d", bfRecordSequence.Add(1))
	payload, err := base.EncodeRecordCells(cells)
	if err != nil {
		t.Fatalf("base.EncodeRecordCells() error = %v", err)
	}
	if _, err := harness.db.Exec(`
		INSERT INTO excel_records (
			id, table_id, cells_json, created_by, created_at, version, soft_deleted_at
		) VALUES (?, ?, ?, ?, 1, 1, NULL)
	`, recordID, tableID, payload, bfCreator); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	return recordID
}

// drainBackfill 反复转发 outbox 并等待条件满足：链式批次事件在消费事务内
// 生成，需要多轮转发；超时即失败，绝不静默放行。
func (harness *backfillHarness) drainBackfill(t *testing.T, done func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
			t.Fatalf("ForwardOnce(%s) error = %v", description, err)
		}
		if done() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backfill drain timeout waiting for %s", description)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// materializedCount 统计一个字段的物化行数。
func (harness *backfillHarness) materializedCount(t *testing.T, fieldID string) int {
	t.Helper()
	var count int
	if err := harness.db.QueryRow(`
		SELECT COUNT(*) FROM excel_calc_materialized WHERE field_id = ?
	`, fieldID).Scan(&count); err != nil {
		t.Fatalf("count materialized rows for %s: %v", fieldID, err)
	}
	return count
}

// structureCalcJobPayloads 读取夹具命名空间下 producer=structure 的 calc.job
// 载荷（按 event_id 升序）。
func (harness *backfillHarness) structureCalcJobPayloads(t *testing.T) []map[string]any {
	t.Helper()
	rows, err := harness.db.Query(`
		SELECT payload FROM excel_bus_outbox
		WHERE base_id = ? AND type = 'calc.job' AND producer = 'structure'
		ORDER BY event_id
	`, bfBaseID)
	if err != nil {
		t.Fatalf("query structure calc.job payloads: %v", err)
	}
	defer rows.Close()
	payloads := make([]map[string]any, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan calc.job payload: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode calc.job payload: %v", err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate calc.job payloads: %v", err)
	}
	return payloads
}

// jobRowsByTable 读取表下全部 calc 作业状态。
func (harness *backfillHarness) jobRowsByTable(t *testing.T, tableID string) map[string]string {
	t.Helper()
	rows, err := harness.db.Query(`
		SELECT job_id, status FROM excel_calc_jobs WHERE table_id = ?
	`, tableID)
	if err != nil {
		t.Fatalf("query calc jobs for %s: %v", tableID, err)
	}
	defer rows.Close()
	jobs := make(map[string]string)
	for rows.Next() {
		var jobID, status string
		if err := rows.Scan(&jobID, &status); err != nil {
			t.Fatalf("scan calc job: %v", err)
		}
		jobs[jobID] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate calc jobs: %v", err)
	}
	return jobs
}

func (harness *backfillHarness) getRecord(
	t *testing.T,
	baseID, recordID string,
) *recordsv1.Record {
	t.Helper()
	response, err := harness.recordsClient.GetRecord(
		recordsReadContext(t),
		&recordsv1.GetRecordRequest{BaseId: baseID, RecordId: recordID},
	)
	if err != nil {
		t.Fatalf("GetRecord(%s) error = %v", recordID, err)
	}
	return response.GetRecord()
}

func bfNumberValue(number int64) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: number}}
}

// TestCreateFormulaFieldBackfillsExistingRecords（少于一批）：含 3 条存量记录
// 的表新建公式字段后，单记录读取返回物化值，不再依赖后续编辑补算。
func TestCreateFormulaFieldBackfillsExistingRecords(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf存量回填")
	source := harness.createField(t, table.GetId(), "数量", structurev1.FieldType_FIELD_TYPE_NUMBER, nil)
	recordIDs := make([]string, 0, 3)
	for index := 0; index < 3; index++ {
		recordIDs = append(recordIDs, harness.seedRecord(t, table.GetId(),
			map[string]*commonv1.Value{source.GetId(): bfNumberValue(2)}))
	}

	formula := harness.createField(t, table.GetId(), "倍数",
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "{数量}*2"})

	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, formula.GetId()) == len(recordIDs)
	}, "all existing records materialized")

	for _, recordID := range recordIDs {
		record := harness.getRecord(t, bfBaseID, recordID)
		value := record.GetCellsMap()[formula.GetId()]
		if value == nil || value.GetIntValue() != 4 {
			t.Fatalf("record %s formula cell = %v, want 4", recordID, value)
		}
	}
	if _, ok := recordCells(t, harness, recordIDs[0])[source.GetId()]; !ok {
		t.Fatal("source cell lost after backfill")
	}

	jobs := harness.jobRowsByTable(t, table.GetId())
	if len(jobs) != 1 {
		t.Fatalf("calc jobs = %d, want 1 (single batch)", len(jobs))
	}
	for jobID, status := range jobs {
		if status != "SUCCESS" {
			t.Fatalf("calc job %s status = %q, want SUCCESS", jobID, status)
		}
	}
	payloads := harness.structureCalcJobPayloads(t)
	if len(payloads) != 1 {
		t.Fatalf("structure calc.job payloads = %d, want 1", len(payloads))
	}
	fieldIDList, _ := payloads[0]["field_id_list"].([]any)
	if len(fieldIDList) != 1 || fieldIDList[0] != formula.GetId() {
		t.Fatalf("calc.job field_id_list = %v, want [%s]", fieldIDList, formula.GetId())
	}
	if _, ok := payloads[0]["backfill"]; !ok {
		t.Fatalf("calc.job payload missing backfill marker: %v", payloads[0])
	}
	if trigger, ok := payloads[0]["trigger"].(map[string]any); !ok || trigger["record_id"] != "" {
		t.Fatalf("calc.job trigger = %v, want empty record (no trigger row)", payloads[0]["trigger"])
	}
}

func recordCells(t *testing.T, harness *backfillHarness, recordID string) map[string]*commonv1.Value {
	t.Helper()
	return harness.getRecord(t, bfBaseID, recordID).GetCellsMap()
}

// TestCreateFormulaFieldBackfillBatchesLargeTable（多批）：501 条存量记录按
// 500/页切成两个作业链式推进，全部物化成功。
func TestCreateFormulaFieldBackfillBatchesLargeTable(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf大表分批")
	source := harness.createField(t, table.GetId(), "数量", structurev1.FieldType_FIELD_TYPE_NUMBER, nil)
	const total = 501
	for index := 0; index < total; index++ {
		harness.seedRecord(t, table.GetId(),
			map[string]*commonv1.Value{source.GetId(): bfNumberValue(2)})
	}

	formula := harness.createField(t, table.GetId(), "倍数",
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "{数量}*2"})

	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, formula.GetId()) == total
	}, "all 501 records materialized across batches")

	jobs := harness.jobRowsByTable(t, table.GetId())
	if len(jobs) != 2 {
		t.Fatalf("calc jobs = %d, want 2 (500 + 1 chain batch)", len(jobs))
	}
	for jobID, status := range jobs {
		if status != "SUCCESS" {
			t.Fatalf("calc job %s status = %q, want SUCCESS", jobID, status)
		}
	}
}

// TestCreateFormulaFieldOnEmptyTableSucceeds（空表）：空表回填为单空作业，
// 成功收尾且无物化行、无链式事件。
func TestCreateFormulaFieldOnEmptyTableSucceeds(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf空表")
	source := harness.createField(t, table.GetId(), "数量", structurev1.FieldType_FIELD_TYPE_NUMBER, nil)
	_ = source

	formula := harness.createField(t, table.GetId(), "倍数",
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "{数量}*2"})

	harness.drainBackfill(t, func() bool {
		jobs := harness.jobRowsByTable(t, table.GetId())
		if len(jobs) != 1 {
			return false
		}
		for _, status := range jobs {
			if status != "SUCCESS" {
				return false
			}
		}
		return true
	}, "empty-table backfill job SUCCESS")

	if count := harness.materializedCount(t, formula.GetId()); count != 0 {
		t.Fatalf("materialized rows = %d, want 0", count)
	}
}

// TestDuplicateFormulaFieldBackfillsExistingRecords：复制公式字段对存量记录
// 同样回填（副本列物化值与源列一致）。
func TestDuplicateFormulaFieldBackfillsExistingRecords(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf复制回填")
	source := harness.createField(t, table.GetId(), "数量", structurev1.FieldType_FIELD_TYPE_NUMBER, nil)
	recordIDs := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		recordIDs = append(recordIDs, harness.seedRecord(t, table.GetId(),
			map[string]*commonv1.Value{source.GetId(): bfNumberValue(3)}))
	}
	origin := harness.createField(t, table.GetId(), "倍数",
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "{数量}*2"})
	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, origin.GetId()) == len(recordIDs)
	}, "origin formula materialized")

	copyResponse, err := harness.structureClient.DuplicateField(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.DuplicateFieldRequest{FieldId: origin.GetId()},
	)
	if err != nil {
		t.Fatalf("DuplicateField() error = %v", err)
	}
	copyField := copyResponse.GetField()

	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, copyField.GetId()) == len(recordIDs)
	}, "duplicated formula materialized")

	for _, recordID := range recordIDs {
		value := recordCells(t, harness, recordID)[copyField.GetId()]
		if value == nil || value.GetIntValue() != 6 {
			t.Fatalf("record %s copy cell = %v, want 6", recordID, value)
		}
	}
}

// TestConvertTextToFormulaBackfillsExistingRecords：文本字段转公式后存量记录
// 回填（转公式路径与新建同口径）。
func TestConvertTextToFormulaBackfillsExistingRecords(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf转公式")
	converted := harness.createField(t, table.GetId(), "备注列", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	recordIDs := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		recordIDs = append(recordIDs, harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{
			converted.GetId(): textValue("占位"),
		}))
	}

	response := harness.convertField(t, converted.GetId(),
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "1+1"}, true)
	if response.GetField().GetType() != structurev1.FieldType_FIELD_TYPE_FORMULA {
		t.Fatalf("converted type = %s, want FIELD_TYPE_FORMULA", response.GetField().GetType())
	}

	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, converted.GetId()) == len(recordIDs)
	}, "converted formula materialized")

	for _, recordID := range recordIDs {
		value := recordCells(t, harness, recordID)[converted.GetId()]
		if value == nil || value.GetIntValue() != 2 {
			t.Fatalf("record %s formula cell = %v, want 2", recordID, value)
		}
	}
}

// TestConvertFormulaToTextClearsMaterializedProjection：转出公式按删除字段
// 级联同口径清理物化列——转换提交即无残留投影，读取为空列起步。
func TestConvertFormulaToTextClearsMaterializedProjection(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf转出清理")
	source := harness.createField(t, table.GetId(), "数量", structurev1.FieldType_FIELD_TYPE_NUMBER, nil)
	recordIDs := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		recordIDs = append(recordIDs, harness.seedRecord(t, table.GetId(),
			map[string]*commonv1.Value{source.GetId(): bfNumberValue(2)}))
	}
	formula := harness.createField(t, table.GetId(), "倍数",
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "{数量}*2"})
	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, formula.GetId()) == len(recordIDs)
	}, "formula materialized before convert-out")

	response := harness.convertField(t, formula.GetId(), structurev1.FieldType_FIELD_TYPE_TEXT, nil, true)
	if response.GetField().GetType() != structurev1.FieldType_FIELD_TYPE_TEXT {
		t.Fatalf("converted type = %s, want FIELD_TYPE_TEXT", response.GetField().GetType())
	}
	if count := harness.materializedCount(t, formula.GetId()); count != 0 {
		t.Fatalf("materialized rows after convert-out = %d, want 0", count)
	}
	for _, recordID := range recordIDs {
		if value := recordCells(t, harness, recordID)[formula.GetId()]; value != nil {
			t.Fatalf("record %s text cell = %v, want absent (empty column)", recordID, value)
		}
	}
}

// TestDeleteFormulaFieldCleansMaterializedProjection：删除公式字段的物化列
// 清理与字段删除同事务生效（与转出公式同口径）。
func TestDeleteFormulaFieldCleansMaterializedProjection(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf删除清理")
	source := harness.createField(t, table.GetId(), "数量", structurev1.FieldType_FIELD_TYPE_NUMBER, nil)
	formula := harness.createField(t, table.GetId(), "倍数",
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "{数量}*2"})
	recordIDs := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		recordIDs = append(recordIDs, harness.seedRecord(t, table.GetId(),
			map[string]*commonv1.Value{source.GetId(): bfNumberValue(2)}))
	}
	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, formula.GetId()) == len(recordIDs)
	}, "formula materialized before delete")

	if _, err := harness.structureClient.DeleteField(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.DeleteFieldRequest{FieldId: formula.GetId(), Confirm: true},
	); err != nil {
		t.Fatalf("DeleteField(confirm) error = %v", err)
	}
	if count := harness.materializedCount(t, formula.GetId()); count != 0 {
		t.Fatalf("materialized rows after delete = %d, want 0", count)
	}
}

// TestBackfillCoexistsWithWriteChainEdits（AC6 顺序化并发模拟）：回填作业消费
// 前存量行被正常编辑，写链同事务物化新值；回填随后按最新数据重算同一值——
// 不改记录版本、不产生 4001 版本冲突、不覆盖丢失。
func TestBackfillCoexistsWithWriteChainEdits(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf并发编辑")
	source := harness.createField(t, table.GetId(), "数量", structurev1.FieldType_FIELD_TYPE_NUMBER, nil)
	first := harness.seedRecord(t, table.GetId(),
		map[string]*commonv1.Value{source.GetId(): bfNumberValue(1)})
	second := harness.seedRecord(t, table.GetId(),
		map[string]*commonv1.Value{source.GetId(): bfNumberValue(2)})

	formula := harness.createField(t, table.GetId(), "倍数",
		structurev1.FieldType_FIELD_TYPE_FORMULA, map[string]any{"expression": "{数量}*2"})

	// 回填尚未消费时，存量行走既有写链编辑（版本 1→2，写链同步物化 20）。
	if _, err := harness.recordsClient.UpdateCell(
		recordsReadContext(t),
		&recordsv1.UpdateCellRequest{
			RecordId:      first,
			FieldId:       source.GetId(),
			Value:         bfNumberValue(10),
			ExpectVersion: 1,
		},
	); err != nil {
		t.Fatalf("UpdateCell(during backfill) error = %v", err)
	}

	harness.drainBackfill(t, func() bool {
		return harness.materializedCount(t, formula.GetId()) == 2
	}, "backfill completed after edit")

	firstRecord := harness.getRecord(t, bfBaseID, first)
	if version := firstRecord.GetVersion(); version != 2 {
		t.Fatalf("edited record version = %d, want 2 (backfill must not bump versions)", version)
	}
	if value := firstRecord.GetCellsMap()[formula.GetId()]; value == nil || value.GetIntValue() != 20 {
		t.Fatalf("edited record formula cell = %v, want 20 (post-edit recomputation)", value)
	}
	secondValue := recordCells(t, harness, second)[formula.GetId()]
	if secondValue == nil || secondValue.GetIntValue() != 4 {
		t.Fatalf("untouched record formula cell = %v, want 4", secondValue)
	}

	// 编辑后再改一次：写链重算仍正常（4001 乐观锁语义不被回填破坏）。
	if _, err := harness.recordsClient.UpdateCell(
		recordsReadContext(t),
		&recordsv1.UpdateCellRequest{
			RecordId:      first,
			FieldId:       source.GetId(),
			Value:         bfNumberValue(5),
			ExpectVersion: 2,
		},
	); err != nil {
		t.Fatalf("UpdateCell(after backfill) error = %v", err)
	}
	finalValue := recordCells(t, harness, first)[formula.GetId()]
	if finalValue == nil || finalValue.GetIntValue() != 10 {
		t.Fatalf("formula cell after second edit = %v, want 10", finalValue)
	}
}

// TestCreateLookupFieldEnqueuesBackfillJob：引用字段创建同样入队回填作业
// （口径统一）；引用值由读路径实时物化，作业空转成功且不留物化行。
func TestCreateLookupFieldEnqueuesBackfillJob(t *testing.T) {
	harness := newBackfillHarness(t)
	table := harness.createTable(t, "bf引用字段")
	title := harness.createField(t, table.GetId(), "抬头", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	harness.seedRecord(t, table.GetId(), map[string]*commonv1.Value{
		title.GetId(): textValue("第一行"),
	})
	relation := harness.createField(t, table.GetId(), "自关联",
		structurev1.FieldType_FIELD_TYPE_RELATION, map[string]any{"target_table_id": table.GetId()})

	lookup := harness.createField(t, table.GetId(), "引用标题",
		structurev1.FieldType_FIELD_TYPE_LOOKUP, map[string]any{
			"relation_field_id": relation.GetId(),
			"target_field_id":   title.GetId(),
		})

	harness.drainBackfill(t, func() bool {
		jobs := harness.jobRowsByTable(t, table.GetId())
		if len(jobs) == 0 {
			return false
		}
		for _, status := range jobs {
			if status != "SUCCESS" {
				return false
			}
		}
		return true
	}, "lookup backfill job SUCCESS")

	if count := harness.materializedCount(t, lookup.GetId()); count != 0 {
		t.Fatalf("materialized rows for lookup = %d, want 0 (read-time materialization)", count)
	}
}
