package records_test

import (
	"context"
	"database/sql"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

func TestBatchUpdateReportsExactOutcomesAndReplaysClientRequest(t *testing.T) {
	formulaConfig, err := structpb.NewStruct(map[string]any{
		"expression": "{Num}+1",
	})
	if err != nil {
		t.Fatalf("formula config: %v", err)
	}
	selectConfig, err := structpb.NewStruct(map[string]any{
		"option_list": []any{
			map[string]any{"id": "opt_a", "name": "A", "color": "blue"},
		},
	})
	if err != nil {
		t.Fatalf("select config: %v", err)
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
		&structurev1.Field{
			Id:        "fld_select",
			TableId:   recordsTestTableID,
			Name:      "Select",
			Type:      structurev1.FieldType_FIELD_TYPE_SELECT,
			ConfigMap: selectConfig,
		},
	)
	evaluator := &writeEvaluator{}
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		evaluator,
	)
	seedRecord(
		t,
		harness.db,
		"rec_batch",
		recordsTestTableID,
		recordsTestUserID,
		1,
		map[string]*commonv1.Value{"fld_num": intCell(1)},
	)
	seedRecord(
		t,
		harness.db,
		"rec_conflict",
		recordsTestTableID,
		recordsTestUserID,
		5,
		map[string]*commonv1.Value{"fld_num": intCell(50)},
	)
	request := &recordsv1.BatchUpdateRequest{
		ClientRequestId: "req_batch_001",
		OpList: []*recordsv1.BatchOperation{
			{RecordId: "rec_batch", FieldId: "fld_num", Value: intCell(2), ExpectVersion: 1},
			{RecordId: "rec_conflict", FieldId: "fld_num", Value: intCell(99), ExpectVersion: 1},
			{RecordId: "rec_batch", FieldId: "fld_formula", Value: intCell(9)},
			{RecordId: "rec_batch", FieldId: "fld_select", Value: stringCell("opt_missing")},
			{RecordId: "rec_batch", FieldId: "fld_num", Value: stringCell("not-a-number")},
			{FieldId: "fld_num", Value: intCell(10)},
		},
	}
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	first, err := harness.client.BatchUpdate(auth, request)
	if err != nil {
		t.Fatalf("BatchUpdate(first) error = %v", err)
	}
	if len(first.GetResultList()) != 6 ||
		len(first.GetFailRowList()) != 2 ||
		first.GetSkippedCount() != 2 {
		t.Fatalf(
			"BatchUpdate outcomes = results:%d fail_rows:%d skipped:%d, want 6/2/2",
			len(first.GetResultList()),
			len(first.GetFailRowList()),
			first.GetSkippedCount(),
		)
	}
	results := resultByIndex(first.GetResultList())
	if !results[0].GetOk() || results[0].GetVersion() != 2 {
		t.Fatalf("result 0 = %v, want success version 2", results[0])
	}
	if results[1].GetOk() ||
		results[1].GetErrorCode() != int32(deal.CodeConcurrentConflict) {
		t.Fatalf("result 1 = %v, want conflict 4001", results[1])
	}
	if results[2].GetOk() || results[3].GetOk() {
		t.Fatalf("read-only/option mismatch results = %v / %v, want skipped", results[2], results[3])
	}
	if results[4].GetOk() ||
		results[4].GetErrorCode() != int32(deal.CodeInvalidArgument) {
		t.Fatalf("result 4 = %v, want invalid type 1001", results[4])
	}
	if !results[5].GetOk() || results[5].GetRecordId() == "" ||
		results[5].GetVersion() != 1 {
		t.Fatalf("result 5 = %v, want created record version 1", results[5])
	}
	conflictRecord, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_conflict",
	)
	if err != nil {
		t.Fatalf("RecordByID(conflict) error = %v", err)
	}
	if conflictRecord.Version != 5 || conflictRecord.Cells["fld_num"].GetIntValue() != 50 {
		t.Fatalf("conflicting row was overwritten: %+v", conflictRecord)
	}

	second, err := harness.client.BatchUpdate(auth, request)
	if err != nil {
		t.Fatalf("BatchUpdate(replay) error = %v", err)
	}
	if !proto.Equal(first, second) {
		t.Fatalf("BatchUpdate replay = %v, want %v", second, first)
	}
	updated, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_batch",
	)
	if err != nil {
		t.Fatalf("RecordByID(updated) error = %v", err)
	}
	if updated.Version != 2 || updated.Cells["fld_num"].GetIntValue() != 2 {
		t.Fatalf("record after replay = %+v, want version 2 value 2", updated)
	}
	assertDatabaseCount(t, harness.db, "excel_records_oplog", 1)
}

func resultByIndex(
	results []*recordsv1.OpResult,
) map[int32]*recordsv1.OpResult {
	indexed := make(map[int32]*recordsv1.OpResult, len(results))
	for _, result := range results {
		indexed[result.GetIndex()] = result
	}
	return indexed
}

func assertDatabaseCount(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM "+table,
	).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s row count = %d, want %d", table, count, want)
	}
}
