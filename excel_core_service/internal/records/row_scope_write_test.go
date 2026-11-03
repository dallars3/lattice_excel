package records_test

import (
	"context"
	"testing"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

func TestUpdateCellRowScopeOwnRejectsAnotherCreatorsRecord(t *testing.T) {
	access := writeStructureAccessor(&structurev1.Field{
		Id:        "fld_score",
		TableId:   recordsTestTableID,
		Name:      "Score",
		Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
		IsPrimary: true,
	})
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_OWN"},
		nil,
	)
	seedRecord(
		t,
		harness.db,
		"rec_other_owner",
		recordsTestTableID,
		recordsTestOtherID,
		1,
		map[string]*commonv1.Value{"fld_score": intCell(5)},
	)
	_, err := harness.client.UpdateCell(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN"),
		&recordsv1.UpdateCellRequest{
			RecordId:      "rec_other_owner",
			FieldId:       "fld_score",
			Value:         intCell(99),
			ExpectVersion: 1,
		},
	)
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)
	record, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_other_owner",
	)
	if err != nil {
		t.Fatalf("RecordByID() error = %v", err)
	}
	if record.Version != 1 || record.Cells["fld_score"].GetIntValue() != 5 {
		t.Fatalf("other-owner record changed: %+v", record)
	}
}
