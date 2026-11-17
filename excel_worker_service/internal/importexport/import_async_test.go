// importexport/import_async_test.go（tfx）：CreateImport 早返回与后台写行的
// 取消矩阵（TC-140-08「导入中隔离」的服务端契约，缺陷 #25 / spec §5.2）。
//
//   - 客户端保持连接：create 在行写完之前即返回 IMPORTING 任务（含 base_id），
//     行在后台继续写入，最终推进到 DONE 并发出 import.finished；
//   - 写行途中请求上下文被取消（进程内直调 = 真实断连信号）：任务转 FAILED，
//     半成品 Base 被清理，失败收尾不随取消一起被取消。
//
// 经 gRPC 到达的调用在早期响应送达后由传输层取消 handler ctx（grpc-go 行为，
// 与断连无关），后台写行必须不受其影响 —— 由走 bufconn 客户端的既有用例锁定。
package importexport_test

import (
	"testing"
	"time"

	importexportv1 "excel/api/importexport/v1"
)

// waitForImportTerminal 轮询导入任务直至到达期望终态（DONE/FAILED）。
// 后台写行以终态落库为完成信号，测试据此与 goroutine 建立先后关系。
func waitForImportTerminal(
	t *testing.T,
	harness *importExportHarness,
	taskID string,
	want importexportv1.ImportStatus,
) *importexportv1.ImportTask {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status, err := harness.client.GetImportStatus(harness.ctx, &importexportv1.GetImportStatusRequest{
			TaskId: taskID,
		})
		if err == nil {
			task := status.GetImportTask()
			if task.GetStatus() == want {
				return task
			}
			if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING {
				t.Fatalf("import task = %+v, want %s", task, want)
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("import task %s never reached %s within 15s", taskID, want)
	return nil // 不可达：Fatalf 终止测试
}

// TestCreateImportReturnsImportingTaskBeforeRowsFinishCreateImport 在行未写完时
// 即返回 IMPORTING 任务（tfx/TC-140-08）：响应先于写行返回且 base_id 已带出，
// 行数为请求时刻已知值；随后后台批量写行完成，任务推进到 DONE。
func TestCreateImportReturnsImportingTaskBeforeRowsFinishCreateImport(t *testing.T) {
	harness := newImportExportHarness(t)
	// 闸门：首个批量建行调用阻塞，模拟 20k 行导入的慢写。
	harness.records.blockFirstCreate = make(chan struct{})
	fileID := seedCSVImportFile(t, harness, "async_early.csv", []byte("名称,数量\n甲,1\n乙,2\n丙,3\n"))
	ctx := incomingImportContext(t, harness.userID, harness.enterpriseID)

	type createResult struct {
		task *importexportv1.ImportTask
		err  error
	}
	resultCh := make(chan createResult, 1)
	go func() {
		created, err := harness.service.CreateImport(ctx, &importexportv1.CreateImportRequest{
			FileId:           fileID,
			FirstRowHeader:   true,
			ColumnConfigList: csvPeopleColumns(),
			ClientRequestId:  "import-tfx-early-001",
		})
		if created == nil {
			resultCh <- createResult{nil, err}
			return
		}
		resultCh <- createResult{created.GetImportTask(), err}
	}()

	var task *importexportv1.ImportTask
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("CreateImport() error = %v, want early IMPORTING response", result.err)
		}
		task = result.task
	case <-time.After(5 * time.Second):
		close(harness.records.blockFirstCreate) // 解除阻塞，让后台写行收尾
		t.Fatal("CreateImport() did not return before rows finished, want early IMPORTING response")
	}
	if task.GetStatus() != importexportv1.ImportStatus_IMPORT_STATUS_IMPORTING {
		t.Fatalf("early task status = %s, want IMPORTING", task.GetStatus())
	}
	if task.GetBaseId() == "" {
		t.Fatal("early task base_id = empty, want the importing Base id")
	}
	if task.GetTotalRows() != 3 {
		t.Fatalf("early task total_rows = %d, want 3", task.GetTotalRows())
	}
	// 响应返回后首个批量写行仍被闸门阻塞：证明写行确实在 create 返回之后才继续。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && harness.records.batchAttempts == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if harness.records.batchAttempts != 1 {
		t.Fatalf(
			"batch create attempts while first batch blocked = %d, want 1 (background write started)",
			harness.records.batchAttempts,
		)
	}
	close(harness.records.blockFirstCreate)

	done := waitForImportTerminal(t, harness, task.GetId(), importexportv1.ImportStatus_IMPORT_STATUS_DONE)
	if done.GetOkRows() != 3 || done.GetFailRows() != 0 {
		t.Fatalf("done task = %+v, want 3 ok / 0 failed rows", done)
	}
	if len(harness.records.batchRowRequests()) != 3 {
		t.Fatalf("batch create rows = %d, want one per row", len(harness.records.batchRowRequests()))
	}
}

// 断连中断矩阵（写行途中取消请求上下文 → FAILED + Base 清理）见
// import_cleanup_test.go 的 TestCreateImportMarksTaskFailedAndCleansBaseOnClientDisconnect。
