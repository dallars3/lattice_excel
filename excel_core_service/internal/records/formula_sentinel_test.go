package records_test

import (
	"context"
	"database/sql"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/eventbus"
	calcv1 "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
	"excel_core_service/internal/calc"
	"excel_core_service/internal/records"
)

// 公式错误哨兵夹具：bufconn 上同时注册 records 与 calc（与 cmd 组装一致），
// 计算走真实 calc 求值器，物化走真实 CalcComputedWriter。formulaProvider 模拟
// 生产 H-48/H-49 的公式值提供者（excel_calc_materialized 投影）。
type sentinelHarness struct {
	client         recordsv1.RecordsServiceClient
	calcClient     calcv1.CalcServiceClient
	db             *sql.DB
	now            time.Time
	memoryBus      *eventbus.MemoryBus
	forwarder      *bus.Forwarder
	computedWriter *writeComputedWriter
}

func newSentinelHarness(
	t *testing.T,
	access records.SchemaOrViewAccessor,
) *sentinelHarness {
	t.Helper()
	db, err := base.OpenDB(recordsTestMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetWriteData(t, db)
	t.Cleanup(func() { resetWriteData(t, db) })

	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	memoryBus := eventbus.NewMemoryBus()
	busStore := base.NewBusStore(db)
	calcRepository := base.NewCalcRepository(db)
	evaluator := calc.NewService(calcRepository, nil, nil, nil, nil)
	computedWriter := &writeComputedWriter{
		delegate: records.NewCalcComputedWriter(calcRepository),
	}
	service := records.NewService(
		base.NewRecordsRepository(db),
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		records.NewCalcFormulaValueProvider(calcRepository),
		func() time.Time { return now },
		nil,
		records.WriterOptions{
			Outbox:         bus.NewOutbox(busStore),
			Bases:          &writeBaseContextProvider{baseID: recordsTestBaseID},
			Evaluator:      evaluator,
			ComputedWriter: computedWriter,
		},
	)
	calcService := calc.NewService(calcRepository, nil, nil, nil, nil)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	records.Register(server, service)
	calc.Register(server, calcService)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///records-sentinel-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	return &sentinelHarness{
		client:         recordsv1.NewRecordsServiceClient(connection),
		calcClient:     calcv1.NewCalcServiceClient(connection),
		db:             db,
		now:            now,
		memoryBus:      memoryBus,
		forwarder:      bus.NewForwarder(busStore, memoryBus, bus.ForwarderConfig{}),
		computedWriter: computedWriter,
	}
}

func sentinelFormulaConfig(t *testing.T, expression string) *structpb.Struct {
	t.Helper()
	config, err := structpb.NewStruct(map[string]any{"expression": expression})
	if err != nil {
		t.Fatalf("formula config: %v", err)
	}
	return config
}

func sentinelAccess(t *testing.T, expression string) records.SchemaOrViewAccessor {
	t.Helper()
	return writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_score",
			TableId:   recordsTestTableID,
			Name:      "Score",
			Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
			IsPrimary: true,
		},
		&structurev1.Field{
			Id:      "fld_divisor",
			TableId: recordsTestTableID,
			Name:    "Divisor",
			Type:    structurev1.FieldType_FIELD_TYPE_NUMBER,
		},
		&structurev1.Field{
			Id:        "fld_quota",
			TableId:   recordsTestTableID,
			Name:      "Quota",
			Type:      structurev1.FieldType_FIELD_TYPE_FORMULA,
			ConfigMap: sentinelFormulaConfig(t, expression),
		},
	)
}

// 工单 09 验收一：除数为零时单元格更新成功，公式格落 #ERROR:除数为零，
// 历史（H-55）与重算状态（W-14 数据面 calc.GetRecalcStatus）读到同一事实。
func TestUpdateCellDivisionByZeroPersistsSentinelInsteadOfRejecting(t *testing.T) {
	harness := newSentinelHarness(t, sentinelAccess(t, "{Score}/{Divisor}"))
	seedRecord(
		t,
		harness.db,
		"rec_t09_div0",
		recordsTestTableID,
		recordsTestUserID,
		1,
		map[string]*commonv1.Value{
			"fld_score":   intCell(3),
			"fld_divisor": intCell(0),
		},
	)

	response, err := harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_t09_div0",
			FieldId:       "fld_score",
			Value:         intCell(8),
			ExpectVersion: 1,
		},
	)
	if err != nil {
		t.Fatalf("UpdateCell() error = %v", err)
	}
	if response.GetVersion() != 2 || response.GetValue().GetIntValue() != 8 {
		t.Fatalf("UpdateCell response = %v, want value 8 version 2", response)
	}

	single, err := harness.client.GetRecord(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.GetRecordRequest{RecordId: "rec_t09_div0", BaseId: recordsTestBaseID},
	)
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	cells := single.GetRecord().GetCellsMap()
	if cells["fld_score"].GetIntValue() != 8 || cells["fld_divisor"].GetIntValue() != 0 {
		t.Fatalf("H-49 user cells = %v, want fld_score 8 and fld_divisor 0", cells)
	}
	if got := cells["fld_quota"].GetStringValue(); got != "#ERROR:除数为零" {
		t.Fatalf("H-49 formula cell = %q, want #ERROR:除数为零", got)
	}

	history, err := harness.client.GetHistory(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.GetHistoryRequest{RecordId: "rec_t09_div0"},
	)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	sentinelAfter := ""
	for _, node := range history.GetHistoryList() {
		if node.GetFieldId() == "fld_quota" {
			sentinelAfter = node.GetAfter().GetStringValue()
		}
	}
	if sentinelAfter != "#ERROR:除数为零" {
		t.Fatalf("history fld_quota after = %q, want #ERROR:除数为零", sentinelAfter)
	}

	recalc, err := harness.calcClient.GetRecalcStatus(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&calcv1.GetRecalcStatusRequest{RecordId: "rec_t09_div0", FieldId: "fld_quota"},
	)
	if err != nil {
		t.Fatalf("GetRecalcStatus() error = %v", err)
	}
	if recalc.GetStatus() != calcv1.RecalcStatus_RECALC_STATUS_ERROR ||
		recalc.GetReason() != "除数为零" {
		t.Fatalf(
			"GetRecalcStatus = %s reason %q, want ERROR/除数为零",
			recalc.GetStatus(),
			recalc.GetReason(),
		)
	}
}

// 工单 09 验收二：除数为零时记录创建成功，且 H-48 列表、H-49 单条、H-55 历史
// 读到同一哨兵值（#ERROR:除数为零）。
func TestCreateRecordDivisionByZeroReadsSameSentinelEverywhere(t *testing.T) {
	harness := newSentinelHarness(t, sentinelAccess(t, "{Score}/{Divisor}"))
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")

	created, err := harness.client.CreateRecord(
		auth,
		&recordsv1.CreateRecordRequest{
			TableId: recordsTestTableID,
			ClientRequestId: "t09-create-div0",
			CellsMap: map[string]*commonv1.Value{
				"fld_score":   intCell(10),
				"fld_divisor": intCell(0),
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateRecord() error = %v", err)
	}
	recordID := created.GetRecord().GetId()
	if recordID == "" {
		t.Fatalf("CreateRecord response = %v, want record id", created)
	}

	const sentinel = "#ERROR:除数为零"
	single, err := harness.client.GetRecord(
		auth,
		&recordsv1.GetRecordRequest{RecordId: recordID, BaseId: recordsTestBaseID},
	)
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if got := single.GetRecord().GetCellsMap()["fld_quota"].GetStringValue(); got != sentinel {
		t.Fatalf("H-49 formula cell = %q, want %q", got, sentinel)
	}

	list, err := harness.client.QueryView(
		auth,
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	found := false
	for _, item := range list.GetRecordList() {
		if item.GetId() != recordID {
			continue
		}
		found = true
		if got := item.GetCellsMap()["fld_quota"].GetStringValue(); got != sentinel {
			t.Fatalf("H-48 formula cell = %q, want %q", got, sentinel)
		}
	}
	if !found {
		t.Fatalf("H-48 list did not return record %s", recordID)
	}

	history, err := harness.client.GetHistory(
		auth,
		&recordsv1.GetHistoryRequest{RecordId: recordID},
	)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	historySentinel := ""
	for _, node := range history.GetHistoryList() {
		if node.GetFieldId() == "fld_quota" {
			historySentinel = node.GetAfter().GetStringValue()
		}
	}
	if historySentinel != sentinel {
		t.Fatalf("H-55 formula history after = %q, want %q", historySentinel, sentinel)
	}
}

// 工单 09 验收三（记录侧）：单元格值类型不匹配仍在保存时拒绝（1001），
// 不哨兵化——哨兵只覆盖公式求值失败。
func TestUpdateCellStillRejectsTypeMismatchedValue(t *testing.T) {
	harness := newSentinelHarness(t, sentinelAccess(t, "{Score}/{Divisor}"))
	seedRecord(
		t,
		harness.db,
		"rec_t09_typemismatch",
		recordsTestTableID,
		recordsTestUserID,
		1,
		map[string]*commonv1.Value{
			"fld_score":   intCell(1),
			"fld_divisor": intCell(2),
		},
	)

	_, err := harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_t09_typemismatch",
			FieldId:       "fld_score",
			Value:         stringCell("not-a-number"),
			ExpectVersion: 1,
		},
	)
	requireRPCErrorCode(t, err, deal.CodeInvalidArgument)
}
