package records_test

import (
	"context"
	"errors"
	"testing"
	"time"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

func TestUpdateCellRowScopeOwnRejectsAnotherCreatorsRecord(t *testing.T) {
	harness := newRowScopeWriteHarness(t)
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
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)
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
	assertDatabaseCount(t, harness.db, "excel_records_histories", 0)
}

func TestBatchUpdateRowScopeOwnRejectsAnotherCreatorsRecord(t *testing.T) {
	harness := newRowScopeWriteHarness(t)
	seedRecord(
		t,
		harness.db,
		"rec_other_owner",
		recordsTestTableID,
		recordsTestOtherID,
		3,
		map[string]*commonv1.Value{"fld_score": intCell(7)},
	)
	response, err := harness.client.BatchUpdate(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN"),
		&recordsv1.BatchUpdateRequest{
			ClientRequestId: "req_batch_scope_001",
			OpList: []*recordsv1.BatchOperation{{
				RecordId:      "rec_other_owner",
				FieldId:       "fld_score",
				Value:         intCell(99),
				ExpectVersion: 3,
			}},
		},
	)
	if err != nil {
		t.Fatalf("BatchUpdate() error = %v", err)
	}
	results := resultByIndex(response.GetResultList())
	if len(results) != 1 ||
		results[0].GetOk() ||
		results[0].GetErrorCode() != int32(deal.CodePermissionDenied) {
		t.Fatalf("batch result = %v, want 3001 and not ok", response.GetResultList())
	}
	record, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_other_owner",
	)
	if err != nil {
		t.Fatalf("RecordByID() error = %v", err)
	}
	if record.Version != 3 || record.Cells["fld_score"].GetIntValue() != 7 {
		t.Fatalf("other-owner record changed: %+v", record)
	}
}

func TestDeleteRecordsRowScopeOwnRejectsAnotherCreatorsRecord(t *testing.T) {
	harness := newRowScopeWriteHarness(t)
	seedRecord(
		t,
		harness.db,
		"rec_other_owner",
		recordsTestTableID,
		recordsTestOtherID,
		1,
		map[string]*commonv1.Value{"fld_score": intCell(5)},
	)
	_, err := harness.client.DeleteRecords(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN"),
		&recordsv1.DeleteRecordsRequest{
			RecordIdList:    []string{"rec_other_owner"},
			ClientRequestId: "req_delete_scope_001",
		},
	)
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)
	if _, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_other_owner",
	); err != nil {
		t.Fatalf("other-owner record no longer readable: %v", err)
	}
}

func TestUndoDeleteRowScopeOwnRejectsAnotherCreatorsRecord(t *testing.T) {
	harness := newRowScopeWriteHarness(t)
	seedRecord(
		t,
		harness.db,
		"rec_other_owner",
		recordsTestTableID,
		recordsTestOtherID,
		2,
		map[string]*commonv1.Value{"fld_score": intCell(5)},
	)
	if _, err := harness.db.ExecContext(
		context.Background(),
		"UPDATE excel_records SET soft_deleted_at = ? WHERE id = ?",
		harness.now.UnixMilli(),
		"rec_other_owner",
	); err != nil {
		t.Fatalf("soft delete other-owner record: %v", err)
	}
	_, err := harness.client.UndoDelete(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN"),
		&recordsv1.UndoDeleteRequest{RecordId: "rec_other_owner"},
	)
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)
	_, err = base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_other_owner",
	)
	if !errors.Is(err, base.ErrRecordNotFound) {
		t.Fatalf("other-owner record restored, RecordByID error = %v", err)
	}
}

func TestRollbackRowScopeOwnRejectsAnotherCreatorsRecord(t *testing.T) {
	harness := newRowScopeWriteHarness(t)
	seedRecord(
		t,
		harness.db,
		"rec_other_owner",
		recordsTestTableID,
		recordsTestOtherID,
		4,
		map[string]*commonv1.Value{"fld_score": intCell(5)},
	)
	seedHistory(
		t,
		harness.db,
		"hst_other_owner",
		"rec_other_owner",
		"fld_score",
		intCell(1),
		intCell(5),
		recordsTestOtherID,
		harness.now.Add(-time.Hour).UnixMilli(),
	)
	_, err := harness.client.Rollback(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN"),
		&recordsv1.RollbackRequest{NodeId: "hst_other_owner"},
	)
	requireRPCErrorCode(t, err, deal.CodePermissionDenied)
	record, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_other_owner",
	)
	if err != nil {
		t.Fatalf("RecordByID() error = %v", err)
	}
	if record.Version != 4 || record.Cells["fld_score"].GetIntValue() != 5 {
		t.Fatalf("other-owner record changed: %+v", record)
	}
}

func TestRowScopeOwnMissingOrDeletedRecordsReturnDeletedResource(t *testing.T) {
	harness := newRowScopeWriteHarness(t)
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN")

	_, err := harness.client.UpdateCell(auth, &recordsv1.UpdateCellRequest{
		RecordId: "rec_missing",
		FieldId:  "fld_score",
		Value:    intCell(1),
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	_, err = harness.client.DeleteRecords(auth, &recordsv1.DeleteRecordsRequest{
		RecordIdList:    []string{"rec_missing"},
		ClientRequestId: "req_delete_missing_001",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	_, err = harness.client.UndoDelete(auth, &recordsv1.UndoDeleteRequest{
		RecordId: "rec_missing",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	_, err = harness.client.Rollback(auth, &recordsv1.RollbackRequest{
		NodeId: "hst_missing",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	_, err = harness.client.GetHistory(auth, &recordsv1.GetHistoryRequest{
		RecordId: "rec_missing",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	batch, err := harness.client.BatchUpdate(auth, &recordsv1.BatchUpdateRequest{
		ClientRequestId: "req_batch_missing_001",
		OpList: []*recordsv1.BatchOperation{{
			RecordId: "rec_missing",
			FieldId:  "fld_score",
			Value:    intCell(1),
		}},
	})
	if err != nil {
		t.Fatalf("BatchUpdate() error = %v", err)
	}
	results := resultByIndex(batch.GetResultList())
	if len(results) != 1 ||
		results[0].GetOk() ||
		results[0].GetErrorCode() != int32(deal.CodeDeletedResource) {
		t.Fatalf("batch result = %v, want 4002 and not ok", batch.GetResultList())
	}

	seedRecord(
		t,
		harness.db,
		"rec_deleted_other_owner",
		recordsTestTableID,
		recordsTestOtherID,
		2,
		map[string]*commonv1.Value{"fld_score": intCell(5)},
	)
	if _, err := harness.db.ExecContext(
		context.Background(),
		"UPDATE excel_records SET soft_deleted_at = ? WHERE id = ?",
		harness.now.UnixMilli(),
		"rec_deleted_other_owner",
	); err != nil {
		t.Fatalf("soft delete record: %v", err)
	}
	_, err = harness.client.UpdateCell(auth, &recordsv1.UpdateCellRequest{
		RecordId: "rec_deleted_other_owner",
		FieldId:  "fld_score",
		Value:    intCell(9),
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	_, err = harness.client.GetHistory(auth, &recordsv1.GetHistoryRequest{
		RecordId: "rec_deleted_other_owner",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)
}

func newRowScopeWriteHarness(t *testing.T) *writeHarness {
	t.Helper()
	return newWriteHarness(
		t,
		writeStructureAccessor(&structurev1.Field{
			Id:        "fld_score",
			TableId:   recordsTestTableID,
			Name:      "Score",
			Type:      structurev1.FieldType_FIELD_TYPE_NUMBER,
			IsPrimary: true,
		}),
		queryScopeProvider{scope: "ROW_SCOPE_OWN"},
		nil,
	)
}
