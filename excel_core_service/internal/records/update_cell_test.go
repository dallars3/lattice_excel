package records_test

import (
	"context"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

func TestUpdateCellVersionConflictReturnsCurrentValueWithoutOverwrite(t *testing.T) {
	access := &queryStructureAccessor{
		table: &structurev1.Table{
			Id:     recordsTestTableID,
			BaseId: recordsTestBaseID,
		},
		fields: []*structurev1.Field{{
			Id:        "fld_score",
			TableId:   recordsTestTableID,
			Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
			IsPrimary: true,
		}},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	seedRecord(
		t,
		harness.db,
		"rec_conflict",
		recordsTestTableID,
		recordsTestUserID,
		2,
		map[string]*commonv1.Value{"fld_score": intCell(10)},
	)

	_, err := harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_conflict",
			FieldId:       "fld_score",
			Value:         intCell(99),
			ExpectVersion: 1,
		},
	)
	requireRPCErrorCode(t, err, deal.CodeConcurrentConflict)
	currentValue, currentVersion := conflictData(t, err)
	if currentValue != float64(10) || currentVersion != float64(2) {
		t.Fatalf(
			"conflict data = current_value:%v current_version:%v, want 10 and 2",
			currentValue,
			currentVersion,
		)
	}

	record, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_conflict",
	)
	if err != nil {
		t.Fatalf("RecordByID() error = %v", err)
	}
	if record.Version != 2 || record.Cells["fld_score"].GetIntValue() != 10 {
		t.Fatalf(
			"record after conflict = version:%d value:%v, want version 2 value 10",
			record.Version,
			record.Cells["fld_score"],
		)
	}
}

func conflictData(t *testing.T, err error) (any, any) {
	t.Helper()
	data := rpcErrorData(t, err)
	return data["current_value"], data["current_version"]
}

func rpcErrorData(t *testing.T, err error) map[string]any {
	t.Helper()
	grpcStatus, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	var data map[string]any
	for _, detail := range grpcStatus.Details() {
		if errorDetail, ok := detail.(*commonv1.ErrorDetail); ok {
			data = errorDetail.GetDetailMap().AsMap()
			break
		}
	}
	if data == nil {
		var info *errdetails.ErrorInfo
		for _, detail := range grpcStatus.Details() {
			if candidate, ok := detail.(*errdetails.ErrorInfo); ok {
				info = candidate
				break
			}
		}
		t.Fatalf("error details = %v info=%v, want common.ErrorDetail", grpcStatus.Details(), info)
	}
	return data
}
