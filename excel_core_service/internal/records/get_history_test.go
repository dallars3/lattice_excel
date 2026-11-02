package records_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

func TestGetHistoryUsesNinetyDayWindowFieldValuesAndRowScope(t *testing.T) {
	access := &multiStructureAccessor{
		tables: map[string]*structurev1.Table{
			recordsTestTableID: {
				Id:     recordsTestTableID,
				BaseId: recordsTestBaseID,
			},
		},
		fields: map[string][]*structurev1.Field{
			recordsTestTableID: {
				{
					Id:        "fld_title",
					TableId:   recordsTestTableID,
					Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
					IsPrimary: true,
				},
				{
					Id:      "fld_score",
					TableId: recordsTestTableID,
					Type:    structurev1.FieldType_FIELD_TYPE_NUMBER,
				},
			},
		},
		views:  map[string]*structurev1.View{},
		config: map[string]*structurev1.ViewConfig{},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_OWN"},
		queryFormulaProvider{},
	)
	seedRecord(t, harness.db, "rec_history", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title": stringCell("current"),
	})
	seedHistory(
		t,
		harness.db,
		"hst_title",
		"rec_history",
		"fld_title",
		stringCell("old"),
		stringCell("current"),
		recordsTestUserID,
		harness.now.Add(-time.Hour).UnixMilli(),
	)
	seedHistory(
		t,
		harness.db,
		"hst_score",
		"rec_history",
		"fld_score",
		nil,
		intCell(42),
		recordsTestOtherID,
		harness.now.Add(-2*time.Hour).UnixMilli(),
	)
	seedHistory(
		t,
		harness.db,
		"hst_old",
		"rec_history",
		"fld_title",
		stringCell("ancient"),
		stringCell("stale"),
		recordsTestUserID,
		harness.now.Add(-90*24*time.Hour-time.Millisecond).UnixMilli(),
	)
	seedHistory(
		t,
		harness.db,
		"hst_other",
		"rec_other",
		"fld_title",
		stringCell("other"),
		stringCell("other-new"),
		recordsTestOtherID,
		harness.now.UnixMilli(),
	)

	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN")
	first, err := harness.client.GetHistory(auth, &recordsv1.GetHistoryRequest{
		RecordId: "rec_history",
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("GetHistory(first page) error = %v", err)
	}
	if len(first.GetHistoryList()) != 1 ||
		first.GetHistoryList()[0].GetId() != "hst_title" ||
		!first.GetHasMore() ||
		first.GetNextCursor() == "" {
		t.Fatalf("first history page = %v has_more=%t cursor=%q", first.GetHistoryList(), first.GetHasMore(), first.GetNextCursor())
	}
	node := first.GetHistoryList()[0]
	if node.GetBefore().GetStringValue() != "old" ||
		node.GetAfter().GetStringValue() != "current" ||
		node.GetOperator() != recordsTestUserID {
		t.Fatalf("title history node = %v", node)
	}

	second, err := harness.client.GetHistory(auth, &recordsv1.GetHistoryRequest{
		RecordId: "rec_history",
		Cursor:   first.GetNextCursor(),
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("GetHistory(second page) error = %v", err)
	}
	if len(second.GetHistoryList()) != 1 ||
		second.GetHistoryList()[0].GetId() != "hst_score" ||
		second.GetHistoryList()[0].GetBefore() != nil ||
		second.GetHistoryList()[0].GetAfter().GetIntValue() != 42 ||
		second.GetHasMore() {
		t.Fatalf("second history page = %v has_more=%t", second.GetHistoryList(), second.GetHasMore())
	}

	_, err = harness.client.GetHistory(
		recordsAuthenticatedContext(t, recordsTestOtherID, "ROW_SCOPE_OWN"),
		&recordsv1.GetHistoryRequest{RecordId: "rec_history"},
	)
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	allScope, err := harness.client.GetHistory(
		recordsAuthenticatedContext(t, recordsTestOtherID, "ROW_SCOPE_ALL"),
		&recordsv1.GetHistoryRequest{RecordId: "rec_history", Limit: 100},
	)
	if err != nil {
		t.Fatalf("GetHistory(ROW_SCOPE_ALL) error = %v", err)
	}
	if len(allScope.GetHistoryList()) != 2 {
		t.Fatalf("ROW_SCOPE_ALL history rows = %d, want 2", len(allScope.GetHistoryList()))
	}
}

func seedHistory(
	t *testing.T,
	db *sql.DB,
	id, recordID, fieldID string,
	before, after *commonv1.Value,
	operator string,
	createdAt int64,
) {
	t.Helper()
	var beforeJSON, afterJSON any
	if before != nil {
		payload, err := base.EncodeRecordCell(before)
		if err != nil {
			t.Fatalf("encode history before: %v", err)
		}
		beforeJSON = payload
	}
	if after != nil {
		payload, err := base.EncodeRecordCell(after)
		if err != nil {
			t.Fatalf("encode history after: %v", err)
		}
		afterJSON = payload
	}
	_, err := db.ExecContext(
		context.Background(),
		"INSERT INTO excel_records_histories ("+
			"id, record_id, field_id, `before`, `after`, operator, created_at"+
			") VALUES (?, ?, ?, ?, ?, ?, ?)",
		id,
		recordID,
		fieldID,
		beforeJSON,
		afterJSON,
		operator,
		createdAt,
	)
	if err != nil {
		t.Fatalf("seed history %s: %v", id, err)
	}
}
