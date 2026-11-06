// importexport/import_batch_write_test.go：导入批量写行（BatchCreateRecords）
// 契约。锁定三件事：
//  1. 批内逐行结果按行号映射回错误报告（坏行不拖垮好行）；
//  2. 批量调用传输级失败时回退逐行写（复用同一行级幂等键，不静默丢批）；
//  3. 大 CSV 按 importBatchSize 分批：批次数 = ⌈行数/批次⌉，批次边界保留取消检查。
package importexport_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	importexportv1 "excel/api/importexport/v1"
	recordsv1 "excel/api/records/v1"
	"excel_worker_service/deal"
)

// batchCSV 生成 n 行数据 CSV（名称,数量）。
func batchCSV(n int) []byte {
	rows := []string{"名称,数量"}
	for index := 0; index < n; index++ {
		rows = append(rows, fmt.Sprintf("行%d,%d", index, index))
	}
	return []byte(strings.Join(rows, "\n") + "\n")
}

// TestCreateImportMapsBatchRowFailuresBackToRowReport 批内混合结果：坏行按行号
// 记入报告并计入 FailRows，好行照常成功（行级语义与逐行 CreateRecord 一致）。
func TestCreateImportMapsBatchRowFailuresBackToRowReport(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedCSVImportFile(t, harness, "batch_partial.csv", batchCSV(4))
	importCtx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")

	// 4 行只有一批；批内第 3 行（CSV 第 4 行）被 core 判为失败。
	harness.records.onBatchRespond = func(callIndex int, request *recordsv1.BatchCreateRecordsRequest) (*recordsv1.BatchCreateRecordsResponse, error) {
		response := &recordsv1.BatchCreateRecordsResponse{}
		for index := range request.GetRowList() {
			result := &recordsv1.BatchCreateRecordResult{
				Index:    int32(index),
				Ok:       true,
				RecordId: fmt.Sprintf("record_b%d_%d", callIndex, index),
			}
			if index == 2 {
				result.Ok = false
				result.ErrorCode = deal.CodeInvalidArgument
				result.Reason = "数字格式无效"
			}
			response.ResultList = append(response.ResultList, result)
		}
		return response, nil
	}

	created, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-batch-partial-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := waitForImportTerminal(t, harness, created.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetOkRows() != 3 || task.GetFailRows() != 1 {
		t.Fatalf("task = %+v, want 3 ok / 1 failed rows", task)
	}
	if task.GetReportFileId() == "" {
		t.Fatal("partial batch failure produced no row report")
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
	for _, want := range []string{"行号", "数字格式无效"} {
		if !strings.Contains(reportText, want) {
			t.Fatalf("report = %q, missing %q", reportText, want)
		}
	}
	if !strings.Contains(reportText, "4") {
		t.Fatalf("report = %q, want the CSV row number 4 of the failed row", reportText)
	}
}

// TestCreateImportFallsBackToPerRowWritesWhenBatchFails 批量调用传输级失败：
// 回退逐行 CreateRecord（同一行级 client_request_id），行不丢失、任务照常完成。
func TestCreateImportFallsBackToPerRowWritesWhenBatchFails(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedCSVImportFile(t, harness, "batch_fallback.csv", batchCSV(3))
	importCtx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")
	harness.records.onBatchRespond = func(
		int,
		*recordsv1.BatchCreateRecordsRequest,
	) (*recordsv1.BatchCreateRecordsResponse, error) {
		return nil, errors.New("transport unavailable")
	}

	created, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-batch-fallback-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := waitForImportTerminal(t, harness, created.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetOkRows() != 3 || task.GetFailRows() != 0 {
		t.Fatalf("task = %+v, want fallback writes to complete 3 rows", task)
	}
	if len(harness.records.createRequests) != 3 {
		t.Fatalf("per-row fallback CreateRecord calls = %d, want 3", len(harness.records.createRequests))
	}
	batchRows := harness.records.batchRowRequests()
	if len(batchRows) != 3 {
		t.Fatalf("batch attempts rows = %d, want the same rows as the fallback", len(batchRows))
	}
	for index, row := range batchRows {
		if row.GetClientRequestId() != harness.records.createRequests[index].GetClientRequestId() {
			t.Fatalf(
				"fallback row %d client_request_id = %q, want the batch key %q",
				index,
				harness.records.createRequests[index].GetClientRequestId(),
				row.GetClientRequestId(),
			)
		}
	}
}

// TestCreateImportChunksLargeCSVByBatchSize 2000 行导入按 importBatchSize 分批：
// 批次数 = ⌈2000/200⌉ = 10，每批行数正确且全批成功（写行提速路径的规模闸门，
// 防止意外退化为逐行 gRPC）。
func TestCreateImportChunksLargeCSVByBatchSize(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedCSVImportFile(t, harness, "batch_scale.csv", batchCSV(2000))
	importCtx := importPrincipal(t, harness.userID, harness.enterpriseID, "", "")

	created, err := harness.client.CreateImport(importCtx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-batch-scale-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	task := waitForImportTerminal(t, harness, created.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if task.GetOkRows() != 2000 || task.GetFailRows() != 0 {
		t.Fatalf("task = %+v, want 2000 ok rows", task)
	}
	if len(harness.records.batchRequests) != 10 {
		t.Fatalf("batch create calls = %d, want 10 (2000 rows / 200 per batch)", len(harness.records.batchRequests))
	}
	for index, request := range harness.records.batchRequests {
		if len(request.GetRowList()) != 200 {
			t.Fatalf("batch %d rows = %d, want 200", index, len(request.GetRowList()))
		}
	}
	if len(harness.records.createRequests) != 0 {
		t.Fatalf("per-row CreateRecord calls = %d, want 0 (batch path only)", len(harness.records.createRequests))
	}
}

// TestCreateImportBatchWritesForwardCoreMetadata 批量写行沿用逐行路径的元数据
// 契约：x-row-scope / x-base-id / x-table-id / x-principal 一致转发。
func TestCreateImportBatchWritesForwardCoreMetadata(t *testing.T) {
	harness := newImportExportHarness(t)
	fileID := seedCSVImportFile(t, harness, "batch_meta.csv", batchCSV(1))
	created, err := harness.client.CreateImport(harness.ctx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-batch-meta-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v", err)
	}
	waitForImportTerminal(t, harness, created.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if len(harness.records.batchMetadata) != 1 {
		t.Fatalf("batch calls = %d, want 1", len(harness.records.batchMetadata))
	}
	assertCoreCallMetadata(t, harness.records.batchMetadata[0])
}

// TestWriteImportChunkAbortsOnCanceledContextAfterBatchError 批量调用失败且请求
// 上下文已取消（断连后回退路径）：立即中止并上抛取消错误，转失败路径清理，
// 不把剩余行逐个写成失败后照常完成。
func TestWriteImportChunkAbortsOnCanceledContextAfterBatchError(t *testing.T) {
	harness := newImportExportHarness(t)
	harness.records.failOnCanceledCtx = true
	fileID := seedCSVImportFile(t, harness, "batch_cancel.csv", batchCSV(400))

	ctx, cancel := context.WithCancel(incomingImportContext(t, harness.userID, harness.enterpriseID))
	// 首批发出时断连：批量调用随取消失败，回退逐行路径立即中止。
	harness.records.onBatch = func(callIndex int) {
		if callIndex == 1 {
			cancel()
		}
	}
	created, err := harness.service.CreateImport(ctx, &importexportv1.CreateImportRequest{
		FileId:           fileID,
		FirstRowHeader:   true,
		ColumnConfigList: csvPeopleColumns(),
		ClientRequestId:  "import-batch-cancel-001",
	})
	if err != nil {
		t.Fatalf("CreateImport() error = %v, want early IMPORTING response", err)
	}
	waitForImportTerminal(t, harness, created.GetImportTask().GetId(), importexportv1.ImportStatus_IMPORT_STATUS_FAILED)
	if harness.records.createAttempts != 0 {
		t.Fatalf(
			"fallback CreateRecord attempts = %d, want 0 (canceled before any per-row write)",
			harness.records.createAttempts,
		)
	}
	// failImport 先落 FAILED 再清理半成品 Base（base_id 保留在任务上，与既有
	// 断连矩阵一致），这里单独等清理完成。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && harness.workspace.deleteCalls == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if harness.workspace.deleteCalls != 1 {
		t.Fatalf("DeleteBase calls = %d, want 1 (half-built Base cleaned)", harness.workspace.deleteCalls)
	}
}
