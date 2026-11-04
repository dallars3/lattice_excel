package records_test

import (
	"testing"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
)

func TestQueryViewMaterializesFormulaValuesThroughProvider(t *testing.T) {
	access := formulaAccessor()
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{values: map[string]*commonv1.Value{
			"rec_formula_ready\x00fld_formula": {
				Kind: &commonv1.Value_IntValue{IntValue: 7},
			},
		}},
	)
	seedFormulaRecords(t, harness)

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	for _, record := range response.GetRecordList() {
		value := record.GetCellsMap()["fld_formula"]
		switch record.GetId() {
		case "rec_formula_ready":
			if value.GetIntValue() != 7 {
				t.Fatalf("formula value = %v, want 7", value)
			}
		case "rec_formula_missing":
			if value != nil {
				t.Fatalf("missing formula materialization leaked stale value %v", value)
			}
		}
	}
}

func TestQueryViewOmitsFormulaWhenProviderIsNotWired(t *testing.T) {
	harness := newQueryHarness(
		t,
		formulaAccessor(),
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	seedFormulaRecords(t, harness)

	response, err := harness.client.QueryView(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.QueryViewRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			Limit:   100,
		},
	)
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	for _, record := range response.GetRecordList() {
		if value := record.GetCellsMap()["fld_formula"]; value != nil {
			t.Fatalf("formula without provider = %v, want omitted", value)
		}
	}
}

func formulaAccessor() *multiStructureAccessor {
	return &multiStructureAccessor{
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
					Id:      "fld_formula",
					TableId: recordsTestTableID,
					Type:    structurev1.FieldType_FIELD_TYPE_FORMULA,
				},
			},
		},
		views:  map[string]*structurev1.View{},
		config: map[string]*structurev1.ViewConfig{},
	}
}

func seedFormulaRecords(t *testing.T, harness *queryHarness) {
	t.Helper()
	seedRecord(t, harness.db, "rec_formula_ready", recordsTestTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_title":   stringCell("ready"),
		"fld_formula": intCell(999),
	})
	seedRecord(t, harness.db, "rec_formula_missing", recordsTestTableID, recordsTestUserID, 2, map[string]*commonv1.Value{
		"fld_title":   stringCell("missing"),
		"fld_formula": intCell(999),
	})
}
