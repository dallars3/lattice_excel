// records/batch_create_test.go：BatchCreateRecords（批量建行）契约。锁定导入
// 提速路径的行级语义：单行失败不回滚整批、幂等重放返回既有 record_id、记录/
// 历史/outbox 产物与逐行 CreateRecord 一致。
package records_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// batchCreateRowsLimit 与 proto 契约（1~500 行）一致；超限整批拒绝。
const batchCreateRowsLimit = 500

func TestBatchCreateRecordsReportsPerRowResultsAndPersistsRows(t *testing.T) {
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_title",
			TableId:   recordsTestTableID,
			Name:      "Title",
			Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
			IsPrimary: true,
		},
		&structurev1.Field{
			Id:      "fld_num",
			TableId: recordsTestTableID,
			Name:    "Num",
			Type:    structurev1.FieldType_FIELD_TYPE_NUMBER,
		},
	)
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	request := &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
		RowList: []*recordsv1.BatchCreateRecordRow{
			{
				CellsMap:        map[string]*commonv1.Value{"fld_title": stringCell("行一"), "fld_num": intCell(1)},
				ClientRequestId: "req_import_row_1",
			},
			{
				// 数字列给了文本：按行失败，不回滚整批。
				CellsMap:        map[string]*commonv1.Value{"fld_title": stringCell("行二"), "fld_num": stringCell("not-a-number")},
				ClientRequestId: "req_import_row_2",
			},
			{
				CellsMap:        map[string]*commonv1.Value{"fld_title": stringCell("行三")},
				ClientRequestId: "req_import_row_3",
			},
		},
	}
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	response, err := harness.client.BatchCreateRecords(auth, request)
	if err != nil {
		t.Fatalf("BatchCreateRecords() error = %v", err)
	}
	results := response.GetResultList()
	if len(results) != 3 {
		t.Fatalf("BatchCreateRecords results = %d, want 3", len(results))
	}
	if !results[0].GetOk() || results[0].GetRecordId() == "" {
		t.Fatalf("result 0 = %v, want success with record id", results[0])
	}
	if results[1].GetOk() ||
		results[1].GetErrorCode() != int32(deal.CodeInvalidArgument) ||
		results[1].GetReason() == "" {
		t.Fatalf("result 1 = %v, want per-row failure 1001 with reason", results[1])
	}
	if !results[2].GetOk() || results[2].GetRecordId() == "" {
		t.Fatalf("result 2 = %v, want success with record id", results[2])
	}
	if results[0].GetRecordId() == results[2].GetRecordId() {
		t.Fatalf("record ids collide: %s", results[0].GetRecordId())
	}
	assertDatabaseCount(t, harness.db, "excel_records", 2)
	// 每个成功行：每个写入字段一条历史 + 一条 record.changed 事件。
	assertDatabaseCount(t, harness.db, "excel_records_histories", 3)
	assertDatabaseCount(t, harness.db, "excel_bus_outbox", 2)

	// 同一批幂等键重放：返回既有 record_id，不重复建行。
	replay, err := harness.client.BatchCreateRecords(auth, request)
	if err != nil {
		t.Fatalf("BatchCreateRecords(replay) error = %v", err)
	}
	replayResults := replay.GetResultList()
	if !replayResults[0].GetOk() ||
		replayResults[0].GetRecordId() != results[0].GetRecordId() {
		t.Fatalf("replay result 0 = %v, want idempotent record id %s", replayResults[0], results[0].GetRecordId())
	}
	if replayResults[1].GetOk() {
		t.Fatalf("replay result 1 = %v, want persistent per-row failure", replayResults[1])
	}
	if !replayResults[2].GetOk() ||
		replayResults[2].GetRecordId() != results[2].GetRecordId() {
		t.Fatalf("replay result 2 = %v, want idempotent record id %s", replayResults[2], results[2].GetRecordId())
	}
	assertDatabaseCount(t, harness.db, "excel_records", 2)
}

func TestBatchCreateRecordsRejectsInvalidArguments(t *testing.T) {
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_title",
			TableId:   recordsTestTableID,
			Name:      "Title",
			Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
			IsPrimary: true,
		},
	)
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")

	if _, err := harness.client.BatchCreateRecords(auth, &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty row list error = %v, want InvalidArgument", err)
	}

	tooMany := make([]*recordsv1.BatchCreateRecordRow, batchCreateRowsLimit+1)
	for index := range tooMany {
		tooMany[index] = &recordsv1.BatchCreateRecordRow{
			CellsMap:        map[string]*commonv1.Value{"fld_title": stringCell("x")},
			ClientRequestId: fmt.Sprintf("req_too_many_%d", index),
		}
	}
	if _, err := harness.client.BatchCreateRecords(auth, &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
		RowList: tooMany,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized batch error = %v, want InvalidArgument", err)
	}

	if _, err := harness.client.BatchCreateRecords(auth, &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
		RowList: []*recordsv1.BatchCreateRecordRow{
			{CellsMap: map[string]*commonv1.Value{"fld_title": stringCell("a")}, ClientRequestId: "req_dup"},
			{CellsMap: map[string]*commonv1.Value{"fld_title": stringCell("b")}, ClientRequestId: "req_dup"},
		},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate client_request_id error = %v, want InvalidArgument", err)
	}
	assertDatabaseCount(t, harness.db, "excel_records", 0)
}

func TestBatchCreateRecordsUnknownFieldFailsSingleRow(t *testing.T) {
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_title",
			TableId:   recordsTestTableID,
			Name:      "Title",
			Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
			IsPrimary: true,
		},
	)
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	response, err := harness.client.BatchCreateRecords(auth, &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
		RowList: []*recordsv1.BatchCreateRecordRow{
			{CellsMap: map[string]*commonv1.Value{"fld_missing": stringCell("x")}, ClientRequestId: "req_unknown_field"},
			{CellsMap: map[string]*commonv1.Value{"fld_title": stringCell("ok")}, ClientRequestId: "req_known_field"},
		},
	})
	if err != nil {
		t.Fatalf("BatchCreateRecords() error = %v", err)
	}
	results := response.GetResultList()
	if results[0].GetOk() || results[0].GetErrorCode() != int32(deal.CodeInvalidArgument) {
		t.Fatalf("result 0 = %v, want unknown-field row failure 1001", results[0])
	}
	if !results[1].GetOk() || results[1].GetRecordId() == "" {
		t.Fatalf("result 1 = %v, want success", results[1])
	}
	assertDatabaseCount(t, harness.db, "excel_records", 1)
}

func TestBatchCreateRecordsEvaluatesRowFormulasAndHistories(t *testing.T) {
	formulaConfig, err := structpb.NewStruct(map[string]any{
		"expression": "{Num}+1",
	})
	if err != nil {
		t.Fatalf("formula config: %v", err)
	}
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_num",
			TableId:   recordsTestTableID,
			Name:      "Num",
			Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
			IsPrimary: true,
		},
		&structurev1.Field{
			Id:        "fld_formula",
			TableId:   recordsTestTableID,
			Name:      "Formula",
			Type:      structurev1.FieldType_FIELD_TYPE_FORMULA,
			ConfigMap: formulaConfig,
		},
	)
	evaluator := &writeEvaluator{}
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		evaluator,
	)
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	response, err := harness.client.BatchCreateRecords(auth, &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
		RowList: []*recordsv1.BatchCreateRecordRow{
			{
				CellsMap:        map[string]*commonv1.Value{"fld_num": intCell(1)},
				ClientRequestId: "req_formula_row",
			},
		},
	})
	if err != nil {
		t.Fatalf("BatchCreateRecords() error = %v", err)
	}
	results := response.GetResultList()
	if !results[0].GetOk() || results[0].GetRecordId() == "" {
		t.Fatalf("result 0 = %v, want success", results[0])
	}
	// 输入字段历史 + 公式字段历史 + record.changed 事件。
	assertDatabaseCount(t, harness.db, "excel_records_histories", 2)
	assertDatabaseCount(t, harness.db, "excel_bus_outbox", 1)
	// 公式物化按行写入（#ERROR 哨兵通道同一实现）。
	assertDatabaseCount(t, harness.db, "excel_calc_materialized", 1)
}

func TestBatchCreateRecordsExpiredIdempotencyKeyIsReusable(t *testing.T) {
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_title",
			TableId:   recordsTestTableID,
			Name:      "Title",
			Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
			IsPrimary: true,
		},
	)
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	request := &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
		RowList: []*recordsv1.BatchCreateRecordRow{
			{
				CellsMap:        map[string]*commonv1.Value{"fld_title": stringCell("第一轮")},
				ClientRequestId: "req_batch_expired",
			},
		},
	}
	first, err := harness.client.BatchCreateRecords(auth, request)
	if err != nil {
		t.Fatalf("BatchCreateRecords(first) error = %v", err)
	}
	if _, err := harness.db.ExecContext(context.Background(), `
		UPDATE excel_records_oplog SET created_at = ? WHERE client_request_id = ?
	`, harness.now.Add(-24*time.Hour).UnixMilli(), "req_batch_expired"); err != nil {
		t.Fatalf("expire batch oplog: %v", err)
	}
	second, err := harness.client.BatchCreateRecords(auth, request)
	if err != nil {
		t.Fatalf("BatchCreateRecords(expired replay) error = %v", err)
	}
	if second.GetResultList()[0].GetRecordId() == first.GetResultList()[0].GetRecordId() {
		t.Fatalf("expired idempotency key reused record %s", first.GetResultList()[0].GetRecordId())
	}
	assertDatabaseCount(t, harness.db, "excel_records", 2)
}

func TestBatchCreateRecordsMismatchedKindFailsRowOnly(t *testing.T) {
	access := writeStructureAccessor(
		&structurev1.Field{
			Id:        "fld_title",
			TableId:   recordsTestTableID,
			Name:      "Title",
			Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
			IsPrimary: true,
		},
	)
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	// 先用同键走一次 BatchUpdate（kind=BatchUpdate），再用同键批量建行。
	batchUpdate := &recordsv1.BatchUpdateRequest{
		ClientRequestId: "req_kind_conflict",
		OpList: []*recordsv1.BatchOperation{
			{FieldId: "fld_title", Value: stringCell("seed")},
		},
	}
	if _, err := harness.client.BatchUpdate(auth, batchUpdate); err != nil {
		t.Fatalf("BatchUpdate(seed) error = %v", err)
	}
	response, err := harness.client.BatchCreateRecords(auth, &recordsv1.BatchCreateRecordsRequest{
		TableId: recordsTestTableID,
		RowList: []*recordsv1.BatchCreateRecordRow{
			{CellsMap: map[string]*commonv1.Value{"fld_title": stringCell("x")}, ClientRequestId: "req_kind_conflict"},
			{CellsMap: map[string]*commonv1.Value{"fld_title": stringCell("y")}, ClientRequestId: "req_kind_ok"},
		},
	})
	if err != nil {
		t.Fatalf("BatchCreateRecords(kind conflict) error = %v", err)
	}
	results := response.GetResultList()
	if results[0].GetOk() || results[0].GetErrorCode() != int32(deal.CodeInvalidArgument) {
		t.Fatalf("result 0 = %v, want kind-conflict row failure 1001", results[0])
	}
	if !results[1].GetOk() {
		t.Fatalf("result 1 = %v, want success", results[1])
	}
	assertDatabaseCount(t, harness.db, "excel_records", 2)
}
