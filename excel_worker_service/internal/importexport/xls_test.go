package importexport_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"excel-libs/eventbus"
	importexportv1 "excel/api/importexport/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_worker_service/deal"
)

func seedXLSFile(t *testing.T, harness *importExportHarness, fixture string, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", fixture, err)
	}
	path := "files/" + name
	fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, content, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return seedImportFile(t, harness, name, path, content)
}

func TestPreviewImportDetectsXLSColumnsAndHeader(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedXLSFile(t, harness, "Table.xls", "people.xls")

	response, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	if err != nil {
		t.Fatalf("PreviewImport() error = %v", err)
	}
	preview := response.GetPreview()
	if preview.GetTotalRows() != 11 || !preview.GetFirstRowHeaderHint() {
		t.Fatalf("preview metadata = %+v, want 11 rows and header hint", preview)
	}
	wantTypes := []structurev1.FieldType{
		structurev1.FieldType_FIELD_TYPE_TEXT,
		structurev1.FieldType_FIELD_TYPE_TEXT,
		structurev1.FieldType_FIELD_TYPE_TEXT,
	}
	wantNames := []string{"Code", "Name", "Description"}
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
	}
}

func TestCreateImportXLSAllRowsSucceedAndPublishEvent(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_code", Name: "Code", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
		{Id: "field_name", Name: "Name", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 1},
		{Id: "field_desc", Name: "Description", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 2},
	}
	fileID := seedXLSFile(t, harness, "Table.xls", "codes.xls")

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
			{Index: 0, Name: "Code", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "Name", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 2, Name: "Description", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
		},
		ClientRequestId: "import-xls-full-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := created.GetImportTask()
	// 写行已转入后台（tfx）：create 先返回 IMPORTING，轮询至 DONE 再断言结果。
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING ||
		task.GetBaseId() == "" {
		t.Fatalf("early import task = %+v, want IMPORTING with base_id", task)
	}
	task = waitForImportTerminal(t, harness, task.GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetTotalRows() != 11 ||
		task.GetOkRows() != 11 ||
		task.GetFailRows() != 0 {
		t.Fatalf("import task = %+v", task)
	}
	if len(harness.records.batchRowRequests()) != 11 {
		t.Fatalf("batch create rows = %d, want 11", len(harness.records.batchRowRequests()))
	}
	first := harness.records.batchRowRequests()[0].GetCellsMap()
	if len(first) != 3 ||
		first["field_code"].GetStringValue() != "code1" ||
		first["field_name"].GetStringValue() != "name1" ||
		first["field_desc"].GetStringValue() != "description1" {
		t.Fatalf("first row cells = %#v", first)
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
		payload["ok_rows"] != float64(11) ||
		payload["fail_rows"] != float64(0) ||
		payload["enterprise_id"] != harness.enterpriseID ||
		payload["creator_id"] != harness.userID ||
		payload["operator"] != harness.userID {
		t.Fatalf("import.finished payload = %#v", payload)
	}
}

func TestCreateImportXLSContinuesAfterBadRowAndWritesReport(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_test1", Name: "Test1", Type: structurev1.FieldType_FIELD_TYPE_NUMBER, Position: 0},
		{Id: "field_lorem", Name: "Lorem", Type: structurev1.FieldType_FIELD_TYPE_DATE, Position: 1},
		{Id: "field_ipsum", Name: "Ipsum", Type: structurev1.FieldType_FIELD_TYPE_DATE, Position: 2},
	}
	fileID := seedXLSFile(t, harness, "mixed.xls", "mixed.xls")

	preview, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	if err != nil {
		t.Fatalf("PreviewImport() error = %v", err)
	}
	previewColumns := preview.GetPreview().GetColumnList()
	if len(previewColumns) != 3 ||
		previewColumns[0].GetDetectedType() != structurev1.FieldType_FIELD_TYPE_TEXT ||
		previewColumns[1].GetDetectedType() != structurev1.FieldType_FIELD_TYPE_DATE ||
		previewColumns[2].GetDetectedType() != structurev1.FieldType_FIELD_TYPE_DATE ||
		!preview.GetPreview().GetFirstRowHeaderHint() ||
		preview.GetPreview().GetTotalRows() != 2 {
		t.Fatalf("preview = %+v", preview.GetPreview())
	}

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
			{Index: 0, Name: "Test1", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
			{Index: 1, Name: "Lorem", Type: structurev1.FieldType_FIELD_TYPE_DATE},
			{Index: 2, Name: "Ipsum", Type: structurev1.FieldType_FIELD_TYPE_DATE},
		},
		ClientRequestId: "import-xls-partial-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := created.GetImportTask()
	// 写行已转入后台（tfx）：create 先返回 IMPORTING，轮询至 DONE 再断言结果。
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING ||
		task.GetBaseId() == "" {
		t.Fatalf("early import task = %+v, want IMPORTING with base_id", task)
	}
	task = waitForImportTerminal(t, harness, task.GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetTotalRows() != 2 ||
		task.GetOkRows() != 1 ||
		task.GetFailRows() != 1 ||
		task.GetReportFileId() == "" {
		t.Fatalf("import task = %+v", task)
	}
	if len(harness.records.batchRowRequests()) != 1 {
		t.Fatalf(
			"batch create rows = %d, want 1 successful row",
			len(harness.records.batchRowRequests()),
		)
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
	for _, want := range []string{"行号", "Test1", "Avocado", "数字格式无效"} {
		if !strings.Contains(reportText, want) {
			t.Fatalf("report = %q, missing %q", reportText, want)
		}
	}

	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive(import.finished) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode import.finished payload: %v", err)
	}
	if payload["task_id"] != task.GetId() ||
		payload["base_id"] != harness.baseID ||
		payload["ok_rows"] != float64(1) ||
		payload["fail_rows"] != float64(1) ||
		payload["enterprise_id"] != harness.enterpriseID ||
		payload["report_file_id"] != task.GetReportFileId() ||
		payload["creator_id"] != harness.userID ||
		payload["operator"] != harness.userID {
		t.Fatalf("import.finished payload = %#v", payload)
	}
}

// ---------- xls 导入稳定性（ticket 01 / 缺陷 #10） ----------

// assertXLSContractCode 断言错误走契约信封：业务码精确匹配（1001/5002…），
// 而不是 9001 服务不可用——worker 绝不能因坏文件退化为基础设施错误。
func assertXLSContractCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want contract error envelope")
	}
	grpcStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	if grpcStatus.Code() != codes.InvalidArgument {
		t.Fatalf("grpc code = %s (%v), want InvalidArgument", grpcStatus.Code(), err)
	}
	for _, detail := range grpcStatus.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			if code, convErr := strconv.Atoi(info.GetReason()); convErr == nil {
				if code != want {
					t.Fatalf("business code = %d (%v), want %d", code, err, want)
				}
				return
			}
		}
	}
	t.Fatalf("error %v carries no ErrorInfo business code", err)
}

// seedXLSBytes 把任意字节写为 .xls 导入文件（损坏/伪造夹具就地构造，不落二进制仓库文件）。
func seedXLSBytes(t *testing.T, harness *importExportHarness, name string, content []byte) string {
	t.Helper()
	path := "files/" + name
	fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(fullPath, content, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return seedImportFile(t, harness, name, path, content)
}

// sheetJSFixtureBytes 返回 SheetJS 生成的 legacy.xls 夹具（TC-140-03 同源：
// 根目录项名为 "R"，是缺陷 #10 的 panic 输入）。
func sheetJSFixtureBytes(t *testing.T) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "legacy_sheetjs.xls"))
	if err != nil {
		t.Fatalf("ReadFile(legacy_sheetjs.xls) error = %v", err)
	}
	return content
}

// corruptXLSFAT 把 FAT 扇区改为指定字节（0xFF=FREESECT 链断裂 / 0x00=自环链）。
func corruptXLSFAT(content []byte, fill byte) []byte {
	corrupted := append([]byte(nil), content...)
	copy(corrupted[512:1024], bytes.Repeat([]byte{fill}, 512))
	return corrupted
}

// corruptXLSRootType 把根目录项 Type 从 ROOT(5) 改为 STORAGE(1)：
// Workbook 短流仍在，但解析器找不到 Root Entry（缺陷 #10 的 panic 栈）。
func corruptXLSRootType(content []byte) []byte {
	corrupted := append([]byte(nil), content...)
	dirstart := binary.LittleEndian.Uint32(content[48:52])
	dirEntry := 512 + dirstart*512
	corrupted[dirEntry+66] = 1
	return corrupted
}

// corruptXLSWorkbookStream 把 Workbook 流内容覆写为乱码（模拟加密/乱码 BIFF 载荷）：
// OLE2 结构完好（校验必须放行），BIFF 解析必须被 panic 兜底拦下。
func corruptXLSWorkbookStream(content []byte) []byte {
	corrupted := append([]byte(nil), content...)
	dirstart := binary.LittleEndian.Uint32(content[48:52])
	rootEntry := 512 + dirstart*512
	rootStart := binary.LittleEndian.Uint32(content[rootEntry+116 : rootEntry+120])
	miniContainer := 512 + rootStart*512
	for i := miniContainer + 64; i < miniContainer+512; i++ {
		corrupted[i] = byte(i * 7)
	}
	return corrupted
}

// TestPreviewImportXLSRejectsNonOLE2Content 伪造的 .xls（非 OLE2 复合文档）
// 必须返回 1001，不得进入脆弱解析器，更不能让 worker 崩溃。
func TestPreviewImportXLSRejectsNonOLE2Content(t *testing.T) {
	harness := newImportExportHarness(t)
	forged := append([]byte("PK\x03\x04 this is a zip, not an OLE2 compound document"), make([]byte, 600)...)
	fileID := seedXLSBytes(t, harness, "forged.xls", forged)

	_, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	assertXLSContractCode(t, err, deal.CodeInvalidArgument)
}

// TestPreviewImportXLSRejectsCorruptedOLE2Structure 结构损坏的 .xls（FAT 断链/
// FAT 自环/截断/根目录项类型损坏）必须统一 1001，坏文件到不了 BIFF 解析层。
func TestPreviewImportXLSRejectsCorruptedOLE2Structure(t *testing.T) {
	base := sheetJSFixtureBytes(t)
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "fat_freesect_chain_break", content: corruptXLSFAT(base, 0xFF)},
		{name: "fat_self_loop", content: corruptXLSFAT(base, 0x00)},
		{name: "truncated_header", content: base[:300]},
		{name: "root_demoted_to_storage", content: corruptXLSRootType(base)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newImportExportHarness(t)
			fileID := seedXLSBytes(t, harness, "corrupt.xls", test.content)
			_, err := harness.client.PreviewImport(
				harness.ctx,
				&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
			)
			assertXLSContractCode(t, err, deal.CodeInvalidArgument)
		})
	}
}

// TestPreviewImportXLSRecoversParserPanicWithoutRestart OLE2 结构完好但 BIFF
// 载荷损坏（加密/乱码）的文件会让解析器 panic；兜底必须把 panic 转成 1001，
// 且同一 worker 实例无需重启即可继续完成 xls 导入与导出。
func TestPreviewImportXLSRecoversParserPanicWithoutRestart(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedXLSBytes(t, harness, "garbage_biff.xls", corruptXLSWorkbookStream(sheetJSFixtureBytes(t)))

	_, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	assertXLSContractCode(t, err, deal.CodeInvalidArgument)

	// panic 被 recover，进程存活：随后正常 xls 预览/入库与导出照常工作。
	goodFileID := seedXLSFile(t, harness, "Table.xls", "people.xls")
	preview, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: goodFileID, SheetIndex: 0},
	)
	if err != nil {
		t.Fatalf("PreviewImport(after panic) error = %v", err)
	}
	if preview.GetPreview().GetTotalRows() != 11 {
		t.Fatalf("total rows = %d, want 11", preview.GetPreview().GetTotalRows())
	}
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_code", Name: "Code", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
		{Id: "field_name", Name: "Name", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 1},
		{Id: "field_desc", Name: "Description", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 2},
	}
	created, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:         goodFileID,
		FirstRowHeader: true,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "Code", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "Name", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 2, Name: "Description", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
		},
		ClientRequestId: "import-xls-t01-recover",
	})
	if err != nil {
		t.Fatalf("CreateImport(after panic) error = %v", err)
	}
	task := created.GetImportTask()
	// 写行已转入后台（tfx）：create 先返回 IMPORTING，轮询至 DONE 再断言结果。
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING {
		t.Fatalf("early task after panic = %+v, want IMPORTING", task)
	}
	task = waitForImportTerminal(t, harness, task.GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetOkRows() != 11 {
		t.Fatalf("import task after panic = %+v, want DONE 11 ok rows", task)
	}

	harness.records.queryResponses = []*recordsv1.QueryViewResponse{{}}
	exported, err := harness.client.CreateExport(harness.ctx, &importexportv1.CreateExportRequest{
		BaseId:          harness.baseID,
		TableId:         harness.tableID,
		ViewId:          "view_1",
		Format:          importexportv1.ExportFormat_EXPORT_FORMAT_CSV,
		ClientRequestId: "export-xls-t01-recover",
	})
	if err != nil {
		t.Fatalf("CreateExport(after panic) error = %v", err)
	}
	if exported.GetExportTask().GetStatus() != importexportv1.ExportStatus_EXPORT_STATUS_DONE {
		t.Fatalf("export task after panic = %+v, want DONE", exported.GetExportTask())
	}
}

// TestCreateImportSheetJSXLSImportsThreeRows 正常 .xls（SheetJS 产物，与
// TC-140-03 同源）必须与 .xlsx 行为一致：识别列并成功入库 3 行。
func TestCreateImportSheetJSXLSImportsThreeRows(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.structure.fields = []*structurev1.Field{
		{Id: "field_name", Name: "名称", Type: structurev1.FieldType_FIELD_TYPE_TEXT, Position: 0},
		{Id: "field_qty", Name: "数量", Type: structurev1.FieldType_FIELD_TYPE_NUMBER, Position: 1},
	}
	fileID := seedXLSFile(t, harness, "legacy_sheetjs.xls", "legacy.xls")

	preview, err := harness.client.PreviewImport(
		harness.ctx,
		&importexportv1.PreviewImportRequest{FileId: fileID, SheetIndex: 0},
	)
	if err != nil {
		t.Fatalf("PreviewImport() error = %v", err)
	}
	columns := preview.GetPreview().GetColumnList()
	if len(columns) != 2 ||
		columns[0].GetName() != "名称" ||
		columns[1].GetName() != "数量" {
		t.Fatalf("preview columns = %+v", columns)
	}

	created, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:         fileID,
		FirstRowHeader: true,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "名称", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "数量", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		ClientRequestId: "import-xls-t01-3rows",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := created.GetImportTask()
	// 写行已转入后台（tfx）：create 先返回 IMPORTING，轮询至 DONE 再断言结果。
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING ||
		task.GetBaseId() == "" {
		t.Fatalf("early import task = %+v, want IMPORTING with base_id", task)
	}
	task = waitForImportTerminal(t, harness, task.GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetTotalRows() != 3 ||
		task.GetOkRows() != 3 ||
		task.GetFailRows() != 0 {
		t.Fatalf("import task = %+v, want DONE 3/3 rows", task)
	}
	if len(harness.records.batchRowRequests()) != 3 {
		t.Fatalf("batch create rows = %d, want 3", len(harness.records.batchRowRequests()))
	}
	names := []string{"甲", "乙", "丙"}
	quantities := []float64{1, 2, 3}
	for index, row := range harness.records.batchRowRequests() {
		cells := row.GetCellsMap()
		if len(cells) != 2 ||
			cells["field_name"].GetStringValue() != names[index] ||
			cells["field_qty"].GetDoubleValue() != quantities[index] {
			t.Fatalf("row %d cells = %#v, want 名称=%s 数量=%v", index, cells, names[index], quantities[index])
		}
	}
}

// TestCreateImportXLSRejectsCorruptedFileWithoutHalfBase 损坏 .xls 走导入
// 新建 Base 路径时必须整体失败（错误信封 1001），不留半成品 Base。
func TestCreateImportXLSRejectsCorruptedFileWithoutHalfBase(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedXLSBytes(t, harness, "corrupt.xls", corruptXLSFAT(sheetJSFixtureBytes(t), 0xFF))
	importCtx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")

	_, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:         fileID,
		FirstRowHeader: true,
		ColumnConfigList: []*importexportv1.ColumnConfig{
			{Index: 0, Name: "名称", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
			{Index: 1, Name: "数量", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
		},
		ClientRequestId: "import-xls-t01-corrupt",
	})
	assertXLSContractCode(t, err, deal.CodeInvalidArgument)
	if harness.workspace.createCalls != 0 {
		t.Fatalf("CreateBase calls = %d, want 0 for an unparsable file", harness.workspace.createCalls)
	}
}
