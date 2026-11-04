// records/capacity_test.go：单表 5 万行容量软上限边界（契约 C-08，5001）。
package records_test

import (

	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
)

func TestCreateRecordRejectsTableOverFiftyThousandRows(t *testing.T) {
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

	// 直插 50000 行到测试表，越过 maxRecordsPerTable 软上限。
	const total = 50000
	const batch = 5000
	for start := 0; start < total; start += batch {
		var builder strings.Builder
		builder.WriteString("INSERT INTO excel_records (id, table_id, cells_json, created_by, created_at, version) VALUES ")
		for index := 0; index < batch; index++ {
			if index > 0 {
				builder.WriteByte(',')
			}
			row := start + index
			builder.WriteString(fmt.Sprintf(
				"('01JROWBULKX%015d', '%s', '{}', '%s', %d, 1)",
				row, recordsTestTableID, recordsTestUserID, time.Now().UnixMilli(),
			))
		}
		if _, err := harness.db.Exec(builder.String()); err != nil {
			t.Fatalf("seed %d records: %v", total, err)
		}
	}

	request := &recordsv1.CreateRecordRequest{
		TableId: recordsTestTableID,
		CellsMap: map[string]*commonv1.Value{
			"fld_title": stringCell("overflow"),
		},
		ClientRequestId: "req_capacity_001",
	}
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")
	_, err := harness.client.CreateRecord(auth, request)
	if err == nil {
		t.Fatal("CreateRecord(over 50000 rows) error = nil, want 5001")
	}
	grpcStatus, ok := status.FromError(err)
	if !ok || grpcStatus.Code() != codes.ResourceExhausted {
		t.Fatalf("CreateRecord(over 50000 rows) code = %v, want ResourceExhausted(5001)", grpcStatus.Code())
	}
	if !strings.Contains(grpcStatus.Message(), "容量") {
		t.Fatalf("message = %q, want capacity wording", grpcStatus.Message())
	}
}
