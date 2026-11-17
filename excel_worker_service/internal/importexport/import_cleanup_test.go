// importexport/import_cleanup_test.go：sheet 越界校验与导入失败清理（ticket 02 /
// 缺陷 #22、#25）。只断言外部行为：错误信封与业务码、任务状态、报告读回、Base 清理。
package importexport_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	importexportv1 "excel/api/importexport/v1"
	structurev1 "excel/api/structure/v1"
	"excel_worker_service/deal"
)

// csvPeopleColumns 单列名映射：名称(text) + 数量(number)。
func csvPeopleColumns() []*importexportv1.ColumnConfig {
	return []*importexportv1.ColumnConfig{
		{Index: 0, Name: "名称", Type: structurev1.FieldType_FIELD_TYPE_TEXT},
		{Index: 1, Name: "数量", Type: structurev1.FieldType_FIELD_TYPE_NUMBER},
	}
}

// incomingImportContext 构造直达 worker 服务的入站元数据上下文（无 x-base-id：
// 走导入新建 Base 路径），测试用它精确取消以模拟客户端断连。
func incomingImportContext(
	t *testing.T,
	userID string,
	enterpriseID string,
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
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"x-principal", base64.StdEncoding.EncodeToString(payload),
			"x-enterprise-id", enterpriseID,
		),
	)
}

// TestCreateImportRejectsOutOfRangeSheetIndexWithoutTask 对单 Sheet 文件传
// sheet_index=5（缺陷 #22，TC-140-06）：返回 1001 且不创建导入任务——同
// client_request_id 修正映射后重试必须真正执行并成功，而不是回放失败任务。
func TestCreateImportRejectsOutOfRangeSheetIndexWithoutTask(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedCSVImportFile(t, harness, "single_sheet.csv", []byte("名称,数量\n苹果,3\n香蕉,5\n橙子,7\n"))
	importCtx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")

	_, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		SheetIndex:       5,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-bounds-001",
	})
	assertXLSContractCode(t, err, deal.CodeInvalidArgument)

	retried, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		SheetIndex:       0,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-bounds-001",
	})
	if err != nil {
		t.Fatalf("CreateImport(retry) error = %v", err)
	}
	// 写行已转入后台（tfx）：轮询至终态再做断言。
	task := waitForImportTerminal(t, harness, retried.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetOkRows() != 3 || task.GetFailRows() != 0 {
		t.Fatalf("retry task = %+v, want fresh DONE import of 3 rows", task)
	}
	if len(harness.records.batchRowRequests()) != 3 {
		t.Fatalf("batch create rows = %d, want 3 (retry executed rows)", len(harness.records.batchRowRequests()))
	}
}

// disconnectCSV 生成 600 行数据 CSV（3 个批次），模拟 20k 行大导入的截断场景。
func disconnectCSV() []byte {
	rows := []string{"名称,数量"}
	for index := 0; index < 600; index++ {
		rows = append(rows, fmt.Sprintf("行%d,%d", index, index))
	}
	return []byte(strings.Join(rows, "\n") + "\n")
}

// TestCreateImportMarksTaskFailedAndCleansBaseOnClientDisconnect 大导入途中客户端
// 断连（缺陷 #25，TC-140-08 中断变体）：写行在后台 goroutine 分批进行（tfx 异步
// 化 + BatchCreateRecords 批量写），途中取消请求上下文（进程内直调 = 真实断连
// 信号）必须立即中止写行、任务转 failed、半成品 Base 被清理 —— 失败收尾（状态
// 更新 + Base 清理）使用独立于请求的 context，不随断连一起被取消；断连后剩余
// 批次不再写入。批量调用本身在断连发生前已带着 IMPORTING 任务早返回。
func TestCreateImportMarksTaskFailedAndCleansBaseOnClientDisconnect(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.records.failOnCanceledCtx = true
	fileID := seedCSVImportFile(t, harness, "disconnect.csv", disconnectCSV())

	ctx, cancel := context.WithCancel(
		incomingImportContext(t, harness.userID, harness.enterpriseID),
	)
	defer cancel()
	// 第 3 批（行 401-600）发出时断连：前 2 批已提交，第 3 批随取消失败。
	harness.records.onBatch = func(callIndex int) {
		if callIndex == 3 {
			cancel()
		}
	}

	created, err := harness.service.CreateImport(ctx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-disconnect-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v, want early IMPORTING response", err)
	}
	if created.GetImportTask().GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING {
		t.Fatalf("early task status = %s, want IMPORTING", created.GetImportTask().GetStatus())
	}
	taskID := created.GetImportTask().GetId()

	waitForImportTerminal(t, harness, taskID, importexportv1.ImportStatus_IMPORT_STATUS_FAILED)
	if harness.records.batchAttempts != 3 {
		t.Fatalf(
			"batch create attempts = %d, want 3 (stopped right at the disconnect)",
			harness.records.batchAttempts,
		)
	}
	if written := len(harness.records.batchRowRequests()); written != 400 {
		t.Fatalf("batch create rows written = %d, want 400 (two committed batches)", written)
	}
	// failImport 先落 FAILED 再清理 Base，这里单独等清理完成。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && harness.workspace.deleteCalls == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if harness.workspace.createCalls != 1 || harness.workspace.deleteCalls != 1 {
		t.Fatalf(
			"CreateBase=%d DeleteBase=%d, want the half-built Base created then cleaned",
			harness.workspace.createCalls,
			harness.workspace.deleteCalls,
		)
	}

	// 断连后用同 client_request_id 重放（公开幂等缝）读回任务：状态必须已是
	// failed（状态更新以独立 context 落库），而不是永远停在导入中。
	replayed, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-disconnect-001",
	})
	if err != nil {
		t.Fatalf("CreateImport(replay) error = %v", err)
	}
	task := replayed.GetImportTask()
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_FAILED {
		t.Fatalf("replayed task status = %s, want FAILED after disconnect", task.GetStatus())
	}
	status, err := harness.client.GetImportStatus(harness.ctx, &importexportv1.GetImportStatusRequest{
		TaskId: task.GetId(),
	})
	if err != nil {
		t.Fatalf("GetImportStatus() error = %v", err)
	}
	if status.GetImportTask().GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_FAILED {
		t.Fatalf("status = %+v, want FAILED", status.GetImportTask())
	}
}

// TestCreateImportCompletesAndPublishesEventAfterLateDisconnect 客户端在收尾瞬间
// 断连（缺陷 #25：断连导致 import.finished 不可靠发出，导入中的 Base 永远锁定）：
// 行全部写完后取消请求上下文，完成态更新与 import.finished 事件必须以独立
// context 可靠落地，任务以 DONE 结束。
func TestCreateImportCompletesAndPublishesEventAfterLateDisconnect(t *testing.T) {
	harness := newImportExportHarness(t)
	// 201 行 = 2 个批次（200+1）：第 2 批（最后一行）发出时断连，该批仍正常
	// 写完（fake 不因取消失败），完成收尾阶段请求上下文已取消。
	fileID := seedCSVImportFile(t, harness, "late_disconnect.csv", batchCSV(201))

	ctx, cancel := context.WithCancel(
		incomingImportContext(t, harness.userID, harness.enterpriseID),
	)
	defer cancel()
	harness.records.onBatch = func(callIndex int) {
		if callIndex == 2 {
			cancel()
		}
	}

	subscribeCtx, cancelSubscribe := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSubscribe()
	subscription, err := harness.events.Subscribe(subscribeCtx, eventbus.SubscribeOptions{
		Topic:     "import.finished",
		Mode:      eventbus.DeliveryModePubSub,
		QueueSize: 1,
	})
	if err != nil {
		t.Fatalf("Subscribe(import.finished) error = %v", err)
	}
	defer subscription.Close()

	created, err := harness.service.CreateImport(ctx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-late-disconnect-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v, want completion despite late disconnect", err)
	}
	// tfx 异步化：create 先返回 IMPORTING；最后一行写完才取消请求上下文，
	// 完成收尾（DONE + import.finished）必须以独立 context 可靠落地。
	if created.GetImportTask().GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING {
		t.Fatalf("early task status = %s, want IMPORTING", created.GetImportTask().GetStatus())
	}
	taskID := created.GetImportTask().GetId()
	waitForImportTerminal(t, harness, taskID, importexportv1.ImportStatus_IMPORT_STATUS_DONE)

	delivery, err := subscription.Receive(subscribeCtx)
	if err != nil {
		t.Fatalf("Receive(import.finished) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode import.finished payload: %v", err)
	}
	if payload["task_id"] != taskID ||
		payload["ok_rows"] != float64(201) ||
		payload["fail_rows"] != float64(0) {
		t.Fatalf("import.finished payload = %#v", payload)
	}

	status, err := harness.client.GetImportStatus(harness.ctx, &importexportv1.GetImportStatusRequest{
		TaskId: taskID,
	})
	if err != nil {
		t.Fatalf("GetImportStatus() error = %v", err)
	}
	if status.GetImportTask().GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_DONE {
		t.Fatalf("status = %+v, want DONE (completion landed despite canceled request)", status.GetImportTask())
	}
}

// twoSheetXLSX 生成两个 Sheet、内容各异的 xlsx 夹具（缺陷 #22 的复现文件形态）。
func twoSheetXLSX(t *testing.T, harness *importExportHarness, name string) string {
	t.Helper()
	path := "files/" + name
	fullPath := filepath.Join(harness.attachDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	workbook := excelize.NewFile()
	if _, err := workbook.NewSheet("第二表"); err != nil {
		t.Fatalf("NewSheet(第二表) error = %v", err)
	}
	sheetRows := map[string][][]any{
		"Sheet1": {{"名称", "数量"}, {"甲", "1"}, {"乙", "2"}, {"丙", "3"}},
		"第二表":  {{"名称", "数量"}, {"丁", "4"}, {"戊", "5"}},
	}
	for sheet, rows := range sheetRows {
		for index, row := range rows {
			cell, _ := excelize.CoordinatesToCellName(1, index+1)
			if err := workbook.SetSheetRow(sheet, cell, &row); err != nil {
				t.Fatalf("SetSheetRow(%s, %d) error = %v", sheet, index, err)
			}
		}
	}
	if err := workbook.SaveAs(fullPath); err != nil {
		t.Fatalf("SaveAs(%s) error = %v", fullPath, err)
	}
	_ = workbook.Close()
	return seedImportFile(t, harness, name, path, nil)
}

// TestCreateImportRejectsOutOfRangeSheetIndexOnTwoSheetXLSX 对 2 个 Sheet 的文件传
// sheet_index=5（缺陷 #22 原始复现）：返回 1001、无导入任务；同 client_request_id
// 修正为 sheet_index=1 后重试必须真正导入第 2 个 Sheet（不得静默回落第 0 个）。
func TestCreateImportRejectsOutOfRangeSheetIndexOnTwoSheetXLSX(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := twoSheetXLSX(t, harness, "two_sheets.xlsx")
	importCtx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")

	_, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		SheetIndex:       5,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-xlsx-bounds-001",
	})
	assertXLSContractCode(t, err, deal.CodeInvalidArgument)

	retried, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		SheetIndex:       1,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-xlsx-bounds-001",
	})
	if err != nil {
		t.Fatalf("CreateImport(retry sheet_index=1) error = %v", err)
	}
	// 写行已转入后台（tfx）：轮询至终态再做断言。
	task := waitForImportTerminal(t, harness, retried.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetOkRows() != 2 || task.GetFailRows() != 0 {
		t.Fatalf("retry task = %+v, want DONE with the 2 rows of sheet 2", task)
	}
	if len(harness.records.batchRowRequests()) != 2 {
		t.Fatalf("batch create rows = %d, want 2 (sheet 2 data rows)", len(harness.records.batchRowRequests()))
	}
	first := harness.records.batchRowRequests()[0].GetCellsMap()
	if first["field_default_title"].GetStringValue() != "丁" ||
		first["created_field_1"].GetDoubleValue() != 4 {
		t.Fatalf("first sheet-2 row cells = %#v, want 丁/4 (not sheet 1 fallback)", first)
	}

	// H-93 预览同口径：越界 Sheet 一律 1001。
	_, err = harness.client.PreviewImport(importCtx, &importexportv1.PreviewImportRequest{
		FileId: fileID, SheetIndex: 5,
	})
	assertXLSContractCode(t, err, deal.CodeInvalidArgument)
	csvFileID := seedCSVImportFile(t, harness, "bounds.csv", []byte("名称,数量\n苹果,3\n"))
	_, err = harness.client.PreviewImport(importCtx, &importexportv1.PreviewImportRequest{
		FileId: csvFileID, SheetIndex: 1,
	})
	assertXLSContractCode(t, err, deal.CodeInvalidArgument)
}

// TestCreateImportRetriesSameFileAfterFailedAttempt 同一 file_id 在一次失败导入
// （任务 failed、半成品 Base 已清理）之后，修正映射以新的 client_request_id 重试
// 必须成功，无需重新上传（缺陷 #22 修复建议 / TC-140-06）。
func TestCreateImportRetriesSameFileAfterFailedAttempt(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.records.createErr = errors.New("records unavailable")
	fileID := seedCSVImportFile(t, harness, "retry_same_file.csv", []byte("名称,数量\n苹果,3\n香蕉,5\n"))
	importCtx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")

	failed, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-retry-fail-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v, want early IMPORTING response", err)
	}
	// 写行在后台进行（tfx）：轮询至 FAILED 后再翻转 fake，避免与后台写行竞争。
	if failed.GetImportTask().GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING {
		t.Fatalf("first attempt status = %s, want IMPORTING", failed.GetImportTask().GetStatus())
	}
	waitForImportTerminal(t, harness, failed.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_FAILED)
	if harness.workspace.deleteCalls != 1 {
		t.Fatalf("DeleteBase calls = %d, want 1 after the failed attempt", harness.workspace.deleteCalls)
	}

	// 修正映射（core 恢复可用）后重试：同一 file_id、新的 client_request_id。
	harness.records.createErr = nil
	retried, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-t02-retry-good-001",
	})
	if err != nil {
		t.Fatalf("CreateImport(retry) error = %v", err)
	}
	task := waitForImportTerminal(t, harness, retried.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetOkRows() != 2 || task.GetFailRows() != 0 {
		t.Fatalf("retry task = %+v, want DONE 2/2 rows without re-upload", task)
	}
	if task.GetId() == failed.GetImportTask().GetId() {
		t.Fatal("retry reused the failed task id, want a fresh task")
	}
}
