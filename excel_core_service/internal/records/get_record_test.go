package records_test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

const recordsTestTargetTableID = "01J00000000000000000002001"

type multiStructureAccessor struct {
	tables map[string]*structurev1.Table
	fields map[string][]*structurev1.Field
	views  map[string]*structurev1.View
	config map[string]*structurev1.ViewConfig
}

func (accessor *multiStructureAccessor) Table(
	_ context.Context,
	tableID string,
) (*structurev1.Table, error) {
	table, ok := accessor.tables[tableID]
	if !ok {
		return nil, fmt.Errorf("table %s not found", tableID)
	}
	return table, nil
}

func (accessor *multiStructureAccessor) Fields(
	_ context.Context,
	tableID string,
) ([]*structurev1.Field, error) {
	fields, ok := accessor.fields[tableID]
	if !ok {
		return nil, fmt.Errorf("fields for %s not found", tableID)
	}
	return fields, nil
}

func (accessor *multiStructureAccessor) View(
	_ context.Context,
	viewID string,
) (*structurev1.View, *structurev1.ViewConfig, error) {
	view, ok := accessor.views[viewID]
	if !ok {
		return nil, nil, fmt.Errorf("view %s not found", viewID)
	}
	return view, accessor.config[viewID], nil
}

func TestRecordsReadsApplyRowScopeToRecordsRelationsAndLookups(t *testing.T) {
	relationConfig, err := structpb.NewStruct(map[string]any{
		"target_table_id": recordsTestTargetTableID,
		"allow_multiple":  true,
	})
	if err != nil {
		t.Fatalf("relation config: %v", err)
	}
	lookupConfig, err := structpb.NewStruct(map[string]any{
		"relation_field_id": "fld_relation",
		"target_field_id":   "fld_target_title",
	})
	if err != nil {
		t.Fatalf("lookup config: %v", err)
	}
	access := &multiStructureAccessor{
		tables: map[string]*structurev1.Table{
			recordsTestTableID: {
				Id:     recordsTestTableID,
				BaseId: recordsTestBaseID,
			},
			recordsTestTargetTableID: {
				Id:     recordsTestTargetTableID,
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
					Id:        "fld_relation",
					TableId:   recordsTestTableID,
					Type:      structurev1.FieldType_FIELD_TYPE_RELATION,
					ConfigMap: relationConfig,
				},
				{
					Id:        "fld_lookup",
					TableId:   recordsTestTableID,
					Type:      structurev1.FieldType_FIELD_TYPE_LOOKUP,
					ConfigMap: lookupConfig,
				},
			},
			recordsTestTargetTableID: {
				{
					Id:        "fld_target_title",
					TableId:   recordsTestTargetTableID,
					Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
					IsPrimary: true,
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
	seedRecord(t, harness.db, "rec_target_visible", recordsTestTargetTableID, recordsTestUserID, 1, map[string]*commonv1.Value{
		"fld_target_title": stringCell("Visible target"),
	})
	seedRecord(t, harness.db, "rec_target_hidden", recordsTestTargetTableID, recordsTestOtherID, 2, map[string]*commonv1.Value{
		"fld_target_title": stringCell("Hidden target"),
	})
	seedRecord(t, harness.db, "rec_source_mine", recordsTestTableID, recordsTestUserID, 3, map[string]*commonv1.Value{
		"fld_title": stringCell("Mine"),
		"fld_relation": listCell(
			stringCell("rec_target_visible"),
			stringCell("rec_target_hidden"),
		),
		"fld_lookup": listCell(
			stringCell("leaked old value"),
		),
	})
	seedRecord(t, harness.db, "rec_source_hidden", recordsTestTableID, recordsTestOtherID, 4, map[string]*commonv1.Value{
		"fld_title": stringCell("Not mine"),
	})

	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN")
	query, err := harness.client.QueryView(auth, &recordsv1.QueryViewRequest{
		BaseId:  recordsTestBaseID,
		TableId: recordsTestTableID,
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("QueryView() error = %v", err)
	}
	if len(query.GetRecordList()) != 1 ||
		query.GetRecordList()[0].GetId() != "rec_source_mine" {
		t.Fatalf("QueryView records = %s, want only rec_source_mine", recordIDs(query.GetRecordList()))
	}
	mine := query.GetRecordList()[0]
	assertRelationIDs(t, mine.GetCellsMap()["fld_relation"], []string{"rec_target_visible"})
	assertLookupStrings(t, mine.GetCellsMap()["fld_lookup"], []string{"Visible target"})

	record, err := harness.client.GetRecord(auth, &recordsv1.GetRecordRequest{
		RecordId: "rec_source_mine",
	})
	if err != nil {
		t.Fatalf("GetRecord(own record) error = %v", err)
	}
	assertRelationIDs(t, record.GetRecord().GetCellsMap()["fld_relation"], []string{"rec_target_visible"})
	assertLookupStrings(t, record.GetRecord().GetCellsMap()["fld_lookup"], []string{"Visible target"})

	_, err = harness.client.GetRecord(auth, &recordsv1.GetRecordRequest{
		RecordId: "rec_source_hidden",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)
	_, err = harness.client.GetRecord(auth, &recordsv1.GetRecordRequest{
		RecordId: "rec_target_hidden",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)

	missingScope := recordsAuthenticatedContext(t, recordsTestUserID, "")
	_, err = harness.client.GetRecord(missingScope, &recordsv1.GetRecordRequest{
		RecordId: "rec_source_mine",
	})
	if err == nil {
		t.Fatal("GetRecord() without x-row-scope succeeded, want explicit error")
	}
}

func recordIDs(records []*recordsv1.Record) []string {
	result := make([]string, 0, len(records))
	for _, record := range records {
		result = append(result, record.GetId())
	}
	return result
}

func assertRelationIDs(
	t *testing.T,
	value *commonv1.Value,
	want []string,
) {
	t.Helper()
	if value == nil {
		t.Fatalf("relation value is missing, want %v", want)
	}
	got := make([]string, 0)
	for _, item := range value.GetListValue().GetValueList() {
		got = append(got, item.GetStringValue())
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("relation ids = %v, want %v", got, want)
	}
}

func assertLookupStrings(
	t *testing.T,
	value *commonv1.Value,
	want []string,
) {
	t.Helper()
	if value == nil {
		t.Fatalf("lookup value is missing, want %v", want)
	}
	got := make([]string, 0)
	for _, item := range value.GetListValue().GetValueList() {
		got = append(got, item.GetStringValue())
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("lookup values = %v, want %v", got, want)
	}
}
