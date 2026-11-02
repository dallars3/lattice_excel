package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
)

func TestDeleteFieldImpactCountsMatchDependencyGraphAndRealRecords(t *testing.T) {
	harness := newStructureHarness(t)
	targetTable := createTable(t, harness, "目标表")
	createField(
		t,
		harness,
		targetTable.GetId(),
		"主键",
		structurev1.FieldType_FIELD_TYPE_TEXT,
		nil,
	)
	deleted := createField(
		t,
		harness,
		targetTable.GetId(),
		"待删除",
		structurev1.FieldType_FIELD_TYPE_TEXT,
		nil,
	)

	dependentTable := createTable(t, harness, "依赖表")
	relation := createField(
		t,
		harness,
		dependentTable.GetId(),
		"目标关联",
		structurev1.FieldType_FIELD_TYPE_RELATION,
		map[string]any{"target_table_id": targetTable.GetId(), "allow_multiple": false},
	)
	lookup := createField(
		t,
		harness,
		dependentTable.GetId(),
		"目标引用",
		structurev1.FieldType_FIELD_TYPE_LOOKUP,
		map[string]any{
			"relation_field_id": relation.GetId(),
			"target_field_id":   deleted.GetId(),
		},
	)
	createField(
		t,
		harness,
		dependentTable.GetId(),
		"跨表公式",
		structurev1.FieldType_FIELD_TYPE_FORMULA,
		map[string]any{"expression": "{目标引用}"},
	)
	harness.impact.records = []string{"rec_a", "rec_b"}

	ctx := authenticatedContext(t, testOwner, testEnterprise)
	preview, err := harness.client.DeleteField(ctx, &structurev1.DeleteFieldRequest{
		FieldId: deleted.GetId(),
		Confirm: false,
	})
	if err != nil {
		t.Fatalf("DeleteField(preview) error = %v", err)
	}
	assertDeleteImpact(t, preview, 2, 1, 1)
	assertFieldExists(t, harness, targetTable.GetId(), deleted.GetId(), true)

	confirmed, err := harness.client.DeleteField(ctx, &structurev1.DeleteFieldRequest{
		FieldId: deleted.GetId(),
		Confirm: true,
	})
	if err != nil {
		t.Fatalf("DeleteField(confirm) error = %v", err)
	}
	assertDeleteImpact(t, confirmed, preview.GetRecordCount(),
		preview.GetDependentFormulas(), preview.GetDependentLookups())
	assertFieldExists(t, harness, targetTable.GetId(), deleted.GetId(), false)

	if lookup.GetName() != "目标引用" {
		t.Fatalf("lookup name = %q", lookup.GetName())
	}
}

func assertDeleteImpact(
	t *testing.T,
	response *structurev1.DeleteFieldResponse,
	records, formulas, lookups int32,
) {
	t.Helper()
	if response.GetRecordCount() != records ||
		response.GetDependentFormulas() != formulas ||
		response.GetDependentLookups() != lookups {
		t.Fatalf("impact = (%d,%d,%d), want (%d,%d,%d)",
			response.GetRecordCount(),
			response.GetDependentFormulas(),
			response.GetDependentLookups(),
			records,
			formulas,
			lookups,
		)
	}
}

func assertFieldExists(
	t *testing.T,
	harness *structureHarness,
	tableID, fieldID string,
	want bool,
) {
	t.Helper()
	response, err := harness.client.ListFields(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	found := false
	for _, field := range response.GetFieldList() {
		if field.GetId() == fieldID {
			found = true
			break
		}
	}
	if found != want {
		t.Fatalf("field %s exists = %v, want %v", fieldID, found, want)
	}
}
