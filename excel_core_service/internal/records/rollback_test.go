package records_test

import (
	"context"
	"testing"
	"time"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

func TestRollbackCreatesNewHistoryNodeAndPreservesOldChain(t *testing.T) {
	access := writeStructureAccessor(&structurev1.Field{
		Id:        "fld_title",
		TableId:   recordsTestTableID,
		Name:      "Title",
		Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
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
		"rec_rollback",
		recordsTestTableID,
		recordsTestUserID,
		3,
		map[string]*commonv1.Value{"fld_title": stringCell("current")},
	)
	seedHistory(
		t,
		harness.db,
		"history_old",
		"rec_rollback",
		"fld_title",
		stringCell("old"),
		stringCell("current"),
		recordsTestUserID,
		harness.now.Add(-time.Hour).UnixMilli(),
	)

	response, err := harness.client.Rollback(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN"),
		&recordsv1.RollbackRequest{NodeId: "history_old"},
	)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	record := response.GetRecord()
	if record.GetId() != "rec_rollback" ||
		record.GetVersion() != 4 ||
		record.GetCellsMap()["fld_title"].GetStringValue() != "old" {
		t.Fatalf("Rollback response = %v, want title old version 4", record)
	}
	histories, err := base.NewRecordsRepository(harness.db).HistoriesByRecord(
		context.Background(),
		"rec_rollback",
		0,
	)
	if err != nil {
		t.Fatalf("HistoriesByRecord() error = %v", err)
	}
	if len(histories) != 2 {
		t.Fatalf("history count = %d, want old node plus rollback node", len(histories))
	}
	var oldFound, newFound bool
	for _, history := range histories {
		switch history.ID {
		case "history_old":
			oldFound = true
		default:
			newFound = newFound ||
				(history.FieldID == "fld_title" &&
					history.Before.GetStringValue() == "current" &&
					history.After.GetStringValue() == "old")
		}
	}
	if !oldFound || !newFound {
		t.Fatalf("histories = %+v, want preserved old node and new rollback node", histories)
	}
}
