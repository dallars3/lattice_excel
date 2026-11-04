package importexport_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	importexportv1 "excel/api/importexport/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	workspacev1 "excel/api/workspace/v1"
	"excel_worker_service/base"
	importlogic "excel_worker_service/internal/importexport"
	worker_rpc "excel_worker_service/rpc"
)

type importExportHarness struct {
	db           *sql.DB
	repository   *base.ImportExportRepository
	fileRepo     *base.FileRepository
	service      *importlogic.Service
	client       importexportv1.ImportExportServiceClient
	events       *eventbus.MemoryBus
	records      *fakeRecordsRPC
	workspace    *fakeWorkspaceRPC
	structure    *fakeStructureRPC
	attachDir    string
	userID       string
	enterpriseID string
	baseID       string
	tableID      string
	ctx          context.Context
	clock        time.Time
}

func newImportExportHarness(t *testing.T) *importExportHarness {
	t.Helper()
	config, err := base.LoadConfig(filepath.Join("..", "..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ensureImportExportTables(db); err != nil {
		t.Fatalf("ensure importexport tables: %v", err)
	}

	harness := &importExportHarness{
		db:           db,
		repository:   base.NewImportExportRepository(db),
		fileRepo:     base.NewFileRepository(db),
		events:       eventbus.NewMemoryBus(),
		attachDir:    t.TempDir(),
		userID:       fmt.Sprintf("01J%023d", time.Now().UnixNano()%1000000000000000000),
		enterpriseID: "enterprise_1",
		baseID:       "base_1",
		tableID:      "table_1",
		clock:        time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local),
	}
	harness.records = &fakeRecordsRPC{}
	harness.workspace = &fakeWorkspaceRPC{base: &workspacev1.Base{
		Id:   harness.baseID,
		Name: "客户数据",
	}}
	harness.structure = newFakeStructureRPC()
	harness.service = importlogic.NewService(
		harness.fileRepo,
		harness.repository,
		harness.records,
		harness.workspace,
		harness.structure,
		harness.events,
		harness.attachDir,
		func() time.Time { return harness.clock },
	)
	listener := bufconn.Listen(8 << 20)
	server := grpc.NewServer()
	importlogic.Register(server, harness.service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient(
		"passthrough:///importexport-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	harness.client = importexportv1.NewImportExportServiceClient(connection)
	harness.ctx = importPrincipal(
		t,
		harness.userID,
		harness.enterpriseID,
		harness.baseID,
		harness.tableID,
	)
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM excel_importexport_tasks WHERE id IN (
				SELECT id FROM (
					SELECT id FROM excel_importexport_tasks
					WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.creator_id')) = ?
				) AS owned_tasks
			)`,
			harness.userID,
		)
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM excel_file_meta WHERE uploader = ?`,
			harness.userID,
		)
	})
	return harness
}

func ensureImportExportTables(db *sql.DB) error {
	var exists int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_name = 'excel_importexport_tasks'
	`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return base.MigrateUp(db)
	}
	return nil
}

func TestPreviewImportDetectsXLSXColumnTypesAndHeader(t *testing.T) {
	harness := newImportExportHarness(t)
	xlsxPath := filepath.Join(harness.attachDir, "files", "people.xlsx")
	if err := os.MkdirAll(filepath.Dir(xlsxPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	workbook := excelize.NewFile()
	rows := [][]any{
		{"姓名", "金额", "日期", "网址", "备注"},
		{"Alice", "1,234.50", "2026-10-31", "https://example.com", "memo"},
		{"Bob", "42", "2026/11/01", "http://example.org/a", "second"},
	}
	for index, row := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, index+1)
		if err := workbook.SetSheetRow("Sheet1", cell, &row); err != nil {
			t.Fatalf("SetSheetRow(%d) error = %v", index, err)
		}
	}
	if err := workbook.SaveAs(xlsxPath); err != nil {
		t.Fatalf("SaveAs() error = %v", err)
	}
	fileID := seedImportFile(t, harness, "people.xlsx", "files/people.xlsx", nil)

	response, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	if err != nil {
		t.Fatalf("PreviewImport() error = %v", err)
	}
	preview := response.GetPreview()
	if preview.GetTotalRows() != 2 || !preview.GetFirstRowHeaderHint() {
		t.Fatalf("preview metadata = %+v, want 2 rows and header hint", preview)
	}
	wantTypes := []structurev1.FieldType{
		structurev1.FieldType_FIELD_TYPE_TEXT,
		structurev1.FieldType_FIELD_TYPE_NUMBER,
		structurev1.FieldType_FIELD_TYPE_DATE,
		structurev1.FieldType_FIELD_TYPE_URL,
		structurev1.FieldType_FIELD_TYPE_TEXT,
	}
	wantNames := []string{"姓名", "金额", "日期", "网址", "备注"}
	columns := preview.GetColumnList()
	if len(columns) != len(wantTypes) {
		t.Fatalf("columns = %+v", columns)
	}
	for index := range columns {
		if columns[index].GetDetectedType() != wantTypes[index] ||
			columns[index].GetName() != wantNames[index] {
			t.Fatalf(
				"column %d = type %s name %q, want type %s name %q",
				index,
				columns[index].GetDetectedType(),
				columns[index].GetName(),
				wantTypes[index],
				wantNames[index],
			)
		}
		if len(columns[index].GetSampleValueList()) == 0 {
			t.Fatalf("column %d has no samples", index)
		}
	}
}

func TestPreviewImportParsesUTF8AndGBKCSV(t *testing.T) {
	harness := newImportExportHarness(t)
	tests := []struct {
		name    string
		content []byte
	}{
		{
			name:    "utf8",
			content: []byte("城市,人口\n上海,2489\n北京,2189\n"),
		},
		{
			name:    "gbk",
			content: mustGBK(t, "城市,人口\n上海,2489\n北京,2189\n"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := "files/" + test.name + ".csv"
			fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
				t.Fatalf("MkdirAll() error = %v", err)
			}
			if err := os.WriteFile(fullPath, test.content, 0o640); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			fileID := seedImportFile(t, harness, test.name+".csv", path, test.content)
			response, err := harness.client.PreviewImport(
				harness.ctx,
				&importexportv1.PreviewImportRequest{FileId: fileID},
			)
			if err != nil {
				t.Fatalf("PreviewImport() error = %v", err)
			}
			columns := response.GetPreview().GetColumnList()
			if len(columns) != 2 ||
				columns[0].GetName() != "城市" ||
				columns[1].GetName() != "人口" ||
				columns[1].GetDetectedType() != structurev1.FieldType_FIELD_TYPE_NUMBER {
				t.Fatalf("preview columns = %+v", columns)
			}
			if response.GetPreview().GetTotalRows() != 2 {
				t.Fatalf("total rows = %d, want 2", response.GetPreview().GetTotalRows())
			}
		})
	}
}

func TestCreateImportContinuesAfterBadRowsAndPublishesReportAndEvent(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_name", Name: "姓名", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
		{Id: "field_age", Name: "年龄", Type: structurev1.FieldType_FIELD_TYPE_NUMBER, Position: 1},
	}
	content := []byte("姓名,年龄\nAlice,30\nBob,not-a-number\nCara,25\n")
	path := "files/import-people.csv"
	fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, content, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fileID := seedImportFile(t, harness, "people.csv", path, content)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:     "import.finished",
		Mode:      eventbus.DeliveryModePubSub,
		QueueSize: 1,
	})
	if err != nil {
		t.Fatalf("Subscribe(import.finished) error = %v", err)
	}
	defer subscription.Close()

	created, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:         fileID,
		FirstRowHeader: true,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "姓名", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "年龄", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		ClientRequestId: "import-partial-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := created.GetImportTask()
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_DONE ||
		task.GetTotalRows() != 3 ||
		task.GetOkRows() != 2 ||
		task.GetFailRows() != 1 ||
		task.GetReportFileId() == "" {
		t.Fatalf("import task = %+v", task)
	}
	if len(harness.records.batchRequests) != 2 {
		t.Fatalf("BatchUpdate calls = %d, want 2 successful rows", len(harness.records.batchRequests))
	}

	status, err := harness.client.GetImportStatus(
		harness.ctx,
		&importexportv1.GetImportStatusRequest{TaskId: task.GetId()},
	)
	if err != nil {
		t.Fatalf("GetImportStatus() error = %v", err)
	}
	if status.GetImportTask().GetOkRows() != 2 || status.GetImportTask().GetFailRows() != 1 {
		t.Fatalf("status task = %+v", status.GetImportTask())
	}

	reportStream, err := harness.client.GetImportReport(
		harness.ctx,
		&importexportv1.GetImportReportRequest{TaskId: task.GetId()},
	)
	if err != nil {
		t.Fatalf("GetImportReport() error = %v", err)
	}
	var report []byte
	for {
		chunk, recvErr := reportStream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("GetImportReport.Recv() error = %v", recvErr)
		}
		report = append(report, chunk.GetChunk()...)
	}
	reportText := string(report)
	for _, want := range []string{"行号", "3", "年龄", "not-a-number"} {
		if !strings.Contains(reportText, want) {
			t.Fatalf("report = %q, missing %q", reportText, want)
		}
	}

	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive(import.finished) error = %v", err)
	}
	if delivery.Event.Header.Type != "import.finished" ||
		delivery.Event.Header.EventID == "" ||
		delivery.Event.Header.Producer != "importexport" {
		t.Fatalf("event header = %+v", delivery.Event.Header)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode import.finished payload: %v", err)
	}
	if payload["task_id"] != task.GetId() ||
		payload["base_id"] != harness.baseID ||
		payload["ok_rows"] != float64(2) ||
		payload["fail_rows"] != float64(1) ||
		payload["enterprise_id"] != harness.enterpriseID ||
		payload["report_file_id"] != task.GetReportFileId() ||
		payload["creator_id"] != harness.userID ||
		payload["operator"] != harness.userID {
		t.Fatalf("import.finished payload = %#v", payload)
	}
}

func TestCreateImportParsesGBKCSV(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_city", Name: "城市", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
		{Id: "field_population", Name: "人口", Type: structurev1.FieldType_FIELD_TYPE_NUMBER, Position: 1},
	}
	content := mustGBK(t, "城市,人口\n上海,2489\n北京,2189\n")
	path := "files/gbk.csv"
	fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, content, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fileID := seedImportFile(t, harness, "gbk.csv", path, content)
	created, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:         fileID,
		FirstRowHeader: true,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "城市", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "人口", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
	})
	if err != nil {
		t.Fatalf("CreateImport(GBK) error = %v", err)
	}
	if created.GetImportTask().GetOkRows() != 2 ||
		created.GetImportTask().GetFailRows() != 0 {
		t.Fatalf("GBK import task = %+v", created.GetImportTask())
	}
	if len(harness.records.batchRequests) != 2 {
		t.Fatalf("BatchUpdate calls = %d, want 2", len(harness.records.batchRequests))
	}
	first := harness.records.batchRequests[0].GetOpList()
	if len(first) != 2 ||
		first[0].GetValue().GetStringValue() != "上海" ||
		first[1].GetValue().GetDoubleValue() != 2489 {
		t.Fatalf("first GBK row operations = %+v", first)
	}
}

func TestNewBaseIsNotCreatedWhenImportCannotBeParsed(t *testing.T) {
	harness := newImportExportHarness(t)
	content := []byte("姓名,年龄\n\"Alice,30\n")
	path := "files/broken.csv"
	fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, content, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fileID := seedImportFile(t, harness, "broken.csv", path, content)
	ctx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")
	_, _ = harness.client.CreateImport(ctx, &importexportv1.CreateImportRequest{
		FileId: fileID,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "姓名", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "年龄", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
	})
	if harness.workspace.createCalls != 0 {
		t.Fatalf(
			"CreateBase calls = %d, want 0 before a broken file is fully parsed",
			harness.workspace.createCalls,
		)
	}
}

func TestCreateImportWithoutEnterpriseRecordsDeadLetter(t *testing.T) {
	harness := newImportExportHarness(t)
	ctx := importPrincipal(t, harness.userID, "", harness.baseID, harness.tableID)
	created, err := harness.client.CreateImport(ctx, &importexportv1.CreateImportRequest{
		FileId:          "file_without_enterprise",
		ClientRequestId: "import-dead-letter-1",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	if created.GetImportTask().GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_FAILED {
		t.Fatalf("task status = %s, want failed", created.GetImportTask().GetStatus())
	}
	task, err := harness.repository.Get(context.Background(), created.GetImportTask().GetId())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		t.Fatalf("decode task payload: %v", err)
	}
	if payload["dead_letter"] != true ||
		payload["creator_id"] != harness.userID ||
		payload["dead_letter_reason"] != "x-enterprise-id is required" {
		t.Fatalf("dead-letter payload = %#v", payload)
	}
}

func TestCreateExportUsesCurrentViewNamingAndAuditEvent(t *testing.T) {
	tests := []struct {
		name      string
		format    importexportv1.ExportFormat
		extension string
	}{
		{name: "xlsx", format: importexportv1.ExportFormat_EXPORT_FORMAT_XLSX, extension: "xlsx"},
		{name: "csv", format: importexportv1.ExportFormat_EXPORT_FORMAT_CSV, extension: "csv"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newImportExportHarness(t)
			harness.structure.fields = []*structurev1.Field{
				{Id: "field_name", Name: "姓名", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
				{Id: "field_hidden", Name: "隐藏列", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 1},
				{Id: "field_age", Name: "年龄", Type: structurev1.FieldType_FIELD_TYPE_NUMBER, Position: 2},
			}
			harness.structure.hiddenFields = []string{"field_hidden"}
			harness.records.queryResponses = []*recordsv1.QueryViewResponse{
				{
					RecordList: []*recordsv1.Record{
						recordWithCells(
							"record_1",
							map[string]*commonv1.Value{
								"field_name":   stringValue("Alice"),
								"field_hidden": stringValue("secret"),
								"field_age":    doubleValue(30),
							},
						),
					},
					HasMore:    true,
					NextCursor: "page_2",
				},
				{
					RecordList: []*recordsv1.Record{
						recordWithCells(
							"record_2",
							map[string]*commonv1.Value{
								"field_name":   stringValue("Bob"),
								"field_hidden": stringValue("other"),
								"field_age":    doubleValue(42),
							},
						),
					},
					HasMore: false,
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
				Topic:     "export.audited",
				Mode:      eventbus.DeliveryModePubSub,
				QueueSize: 1,
			})
			if err != nil {
				t.Fatalf("Subscribe(export.audited) error = %v", err)
			}
			defer subscription.Close()

			request := &importexportv1.CreateExportRequest{
				BaseId:          harness.baseID,
				TableId:         harness.tableID,
				ViewId:          "view_1",
				Format:          test.format,
				ClientRequestId: "export-" + test.name,
			}
			created, err := harness.client.CreateExport(harness.ctx, request)
			if err != nil {
				t.Fatalf("CreateExport() error = %v", err)
			}
			task := created.GetExportTask()
			wantName := "客户数据_客户表_全部_20261103." + test.extension
			if task.GetStatus() != importexportv1.ExportStatus_EXPORT_STATUS_DONE ||
				task.GetFileName() != wantName ||
				task.GetRows() != 2 {
				t.Fatalf("export task = %+v, want file %q", task, wantName)
			}
			if harness.records.queryCalls != 2 {
				t.Fatalf("QueryView calls = %d, want 2 cursor pages", harness.records.queryCalls)
			}

			replayed, err := harness.client.CreateExport(harness.ctx, request)
			if err != nil {
				t.Fatalf("CreateExport(replay) error = %v", err)
			}
			if replayed.GetExportTask().GetId() != task.GetId() ||
				harness.records.queryCalls != 2 {
				t.Fatalf("idempotent replay = %+v, query calls=%d", replayed.GetExportTask(), harness.records.queryCalls)
			}

			stream, err := harness.client.ExportStream(
				harness.ctx,
				&importexportv1.ExportStreamRequest{TaskId: task.GetId()},
			)
			if err != nil {
				t.Fatalf("ExportStream() error = %v", err)
			}
			var exported []byte
			for {
				chunk, recvErr := stream.Recv()
				if recvErr == io.EOF {
					break
				}
				if recvErr != nil {
					t.Fatalf("ExportStream.Recv() error = %v", recvErr)
				}
				exported = append(exported, chunk.GetChunk()...)
			}
			if test.extension == "csv" {
				text := string(exported)
				if !strings.Contains(text, "姓名,年龄") ||
					!strings.Contains(text, "Alice,30") ||
					!strings.Contains(text, "Bob,42") ||
					strings.Contains(text, "隐藏列") {
					t.Fatalf("CSV export = %q", text)
				}
			} else {
				workbook, openErr := excelize.OpenReader(bytes.NewReader(exported))
				if openErr != nil {
					t.Fatalf("OpenReader(export) error = %v", openErr)
				}
				rows, rowErr := workbook.GetRows(workbook.GetSheetName(0))
				_ = workbook.Close()
				if rowErr != nil {
					t.Fatalf("GetRows(export) error = %v", rowErr)
				}
				if fmt.Sprint(rows) != fmt.Sprint([][]string{
					{"姓名", "年龄"},
					{"Alice", "30"},
					{"Bob", "42"},
				}) {
					t.Fatalf("XLSX rows = %#v", rows)
				}
			}

			delivery, err := subscription.Receive(ctx)
			if err != nil {
				t.Fatalf("Receive(export.audited) error = %v", err)
			}
			var payload struct {
				Operator     string   `json:"operator"`
				BaseID       string   `json:"base_id"`
				TableID      string   `json:"table_id"`
				ViewID       string   `json:"view_id"`
				Format       string   `json:"format"`
				RowList      []string `json:"row_list"`
				EnterpriseID string   `json:"enterprise_id"`
			}
			if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
				t.Fatalf("decode export.audited payload: %v", err)
			}
			if payload.Operator != harness.userID ||
				payload.BaseID != harness.baseID ||
				payload.TableID != harness.tableID ||
				payload.ViewID != "view_1" ||
				payload.Format != test.name ||
				payload.EnterpriseID != harness.enterpriseID ||
				fmt.Sprint(payload.RowList) != fmt.Sprint([]string{"record_1", "record_2"}) {
				t.Fatalf("export.audited payload = %#v", payload)
			}
		})
	}
}

func seedImportFile(
	t *testing.T,
	harness *importExportHarness,
	name string,
	storagePath string,
	content []byte,
) string {
	t.Helper()
	meta := base.FileMeta{
		ID:          fmt.Sprintf("01J%023d", time.Now().UnixNano()%1000000000000000000),
		Name:        name,
		DisplayName: name,
		Ext:         filepath.Ext(name),
		Mime:        "application/octet-stream",
		Size:        int64(len(content)),
		Usage:       "import_temp",
		Uploader:    harness.userID,
		UploadBase:  harness.baseID,
		StoragePath: storagePath,
		Checksum:    fmt.Sprintf("%064d", time.Now().UnixNano()%1000000000000000000),
		CreatedAt:   time.Now().UnixMilli(),
	}
	if content == nil {
		info, err := os.Stat(filepath.Join(harness.attachDir, filepath.FromSlash(storagePath)))
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", storagePath, err)
		}
		meta.Size = info.Size()
	}
	if meta.Ext == ".csv" {
		meta.Mime = "text/csv"
	}
	if meta.Ext == ".xlsx" {
		meta.Mime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if meta.Ext == ".xls" {
		meta.Mime = "application/vnd.ms-excel"
	}
	if err := harness.fileRepo.Create(context.Background(), meta); err != nil {
		t.Fatalf("Create(%s) error = %v", name, err)
	}
	return meta.ID
}

func importPrincipal(
	t *testing.T,
	userID string,
	enterpriseID string,
	baseID string,
	tableID string,
) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     true,
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal", base64.StdEncoding.EncodeToString(payload),
		"x-enterprise-id", enterpriseID,
		"x-base-id", baseID,
		"x-table-id", tableID,
	)
}

func mustGBK(t *testing.T, value string) []byte {
	t.Helper()
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(value))
	if err != nil {
		t.Fatalf("GBK encode: %v", err)
	}
	return encoded
}

type fakeRecordsRPC struct {
	queryResponses []*recordsv1.QueryViewResponse
	queryCalls     int
	batchRequests  []*recordsv1.BatchUpdateRequest
}

func (fake *fakeRecordsRPC) QueryView(
	context.Context,
	*recordsv1.QueryViewRequest,
) (*recordsv1.QueryViewResponse, error) {
	index := fake.queryCalls
	fake.queryCalls++
	if index >= len(fake.queryResponses) {
		return &recordsv1.QueryViewResponse{}, nil
	}
	return fake.queryResponses[index], nil
}

func (fake *fakeRecordsRPC) BatchUpdate(
	_ context.Context,
	request *recordsv1.BatchUpdateRequest,
) (*recordsv1.BatchUpdateResponse, error) {
	fake.batchRequests = append(fake.batchRequests, request)
	response := &recordsv1.BatchUpdateResponse{}
	for index := range request.GetOpList() {
		response.ResultList = append(response.ResultList, &recordsv1.OpResult{
			Index: int32(index),
			Ok:    true,
		})
	}
	return response, nil
}

var _ worker_rpc.RecordsRPC = (*fakeRecordsRPC)(nil)

type fakeWorkspaceRPC struct {
	base        *workspacev1.Base
	createCalls int
}

func (fake *fakeWorkspaceRPC) GetHome(
	context.Context,
	*emptypb.Empty,
) (*workspacev1.GetHomeResponse, error) {
	return &workspacev1.GetHomeResponse{Home: &workspacev1.Home{
		CreatedList: []*workspacev1.Base{fake.base},
	}}, nil
}

func (fake *fakeWorkspaceRPC) CreateBase(
	context.Context,
	*workspacev1.CreateBaseRequest,
) (*workspacev1.CreateBaseResponse, error) {
	fake.createCalls++
	return &workspacev1.CreateBaseResponse{Base: fake.base}, nil
}

var _ worker_rpc.WorkspaceRPC = (*fakeWorkspaceRPC)(nil)

type fakeStructureRPC struct {
	fields       []*structurev1.Field
	createCalls  int
	hiddenFields []string
}

func newFakeStructureRPC() *fakeStructureRPC {
	return &fakeStructureRPC{
		fields: []*structurev1.Field{
			{Id: "field_name", Name: "姓名", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
			{Id: "field_age", Name: "年龄", Type: structurev1.FieldType_FIELD_TYPE_NUMBER, Position: 1},
		},
	}
}

func (fake *fakeStructureRPC) ListTables(
	context.Context,
	*structurev1.ListTablesRequest,
) (*structurev1.ListTablesResponse, error) {
	return &structurev1.ListTablesResponse{TableList: []*structurev1.Table{{
		Id:   "table_1",
		Name: "客户表",
	}}}, nil
}

func (fake *fakeStructureRPC) CreateTable(
	context.Context,
	*structurev1.CreateTableRequest,
) (*structurev1.CreateTableResponse, error) {
	return &structurev1.CreateTableResponse{Table: &structurev1.Table{
		Id:   "table_1",
		Name: "导入数据",
	}}, nil
}

func (fake *fakeStructureRPC) ListFields(
	context.Context,
	*structurev1.ListFieldsRequest,
) (*structurev1.ListFieldsResponse, error) {
	fields := append([]*structurev1.Field(nil), fake.fields...)
	sort.Slice(fields, func(left, right int) bool {
		return fields[left].GetPosition() < fields[right].GetPosition()
	})
	return &structurev1.ListFieldsResponse{FieldList: fields}, nil
}

func (fake *fakeStructureRPC) CreateField(
	_ context.Context,
	request *structurev1.CreateFieldRequest,
) (*structurev1.CreateFieldResponse, error) {
	fake.createCalls++
	field := &structurev1.Field{
		Id:       fmt.Sprintf("created_field_%d", fake.createCalls),
		Name:     request.GetName(),
		Type:     request.GetType(),
		Position: int32(fake.createCalls - 1),
	}
	fake.fields = append(fake.fields, field)
	return &structurev1.CreateFieldResponse{Field: field}, nil
}

func (fake *fakeStructureRPC) ListViews(
	context.Context,
	*structurev1.ListViewsRequest,
) (*structurev1.ListViewsResponse, error) {
	return &structurev1.ListViewsResponse{ViewList: []*structurev1.View{{
		Id:   "view_1",
		Name: "全部",
	}}}, nil
}

func (fake *fakeStructureRPC) GetViewConfig(
	context.Context,
	*structurev1.GetViewConfigRequest,
) (*structurev1.GetViewConfigResponse, error) {
	return &structurev1.GetViewConfigResponse{
		ViewConfig: &structurev1.ViewConfig{
			HiddenFieldList: fake.hiddenFields,
		},
	}, nil
}

var _ worker_rpc.StructureRPC = (*fakeStructureRPC)(nil)

func recordWithCells(
	recordID string,
	cells map[string]*commonv1.Value,
) *recordsv1.Record {
	return &recordsv1.Record{
		Id:       recordID,
		TableId:  "table_1",
		CellsMap: cells,
	}
}

func stringValue(value string) *commonv1.Value {
	return &commonv1.Value{
		Kind: &commonv1.Value_StringValue{StringValue: value},
	}
}

func doubleValue(value float64) *commonv1.Value {
	return &commonv1.Value{
		Kind: &commonv1.Value_DoubleValue{DoubleValue: value},
	}
}
