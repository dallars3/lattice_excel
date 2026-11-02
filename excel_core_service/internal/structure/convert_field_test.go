package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
)

func TestConvertFieldPreviewMatchesConfirmationAndRejectsDateToNumber(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "转换")
	field := createField(
		t,
		harness,
		table.GetId(),
		"金额",
		structurev1.FieldType_FIELD_TYPE_NUMBER,
		map[string]any{"format": "decimal", "decimals": 2},
	)
	harness.impact.conversionAffected = 2
	harness.impact.conversionCleared = 1
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	preview, err := harness.client.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    field.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_TEXT,
		Confirm:    false,
	})
	if err != nil {
		t.Fatalf("ConvertField(preview) error = %v", err)
	}
	if preview.GetAffectedCount() != 2 || preview.GetClearedCount() != 1 {
		t.Fatalf("preview counts = (%d,%d), want (2,1)",
			preview.GetAffectedCount(), preview.GetClearedCount())
	}
	if got := listFieldType(t, harness, table.GetId(), field.GetId()); got != structurev1.FieldType_FIELD_TYPE_NUMBER {
		t.Fatalf("preview changed field type to %s", got)
	}

	confirmed, err := harness.client.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    field.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_TEXT,
		Confirm:    true,
	})
	if err != nil {
		t.Fatalf("ConvertField(confirm) error = %v", err)
	}
	if confirmed.GetAffectedCount() != preview.GetAffectedCount() ||
		confirmed.GetClearedCount() != preview.GetClearedCount() {
		t.Fatalf("confirmed counts = (%d,%d), preview = (%d,%d)",
			confirmed.GetAffectedCount(),
			confirmed.GetClearedCount(),
			preview.GetAffectedCount(),
			preview.GetClearedCount(),
		)
	}
	if got := listFieldType(t, harness, table.GetId(), field.GetId()); got != structurev1.FieldType_FIELD_TYPE_TEXT {
		t.Fatalf("confirmed field type = %s, want FIELD_TYPE_TEXT", got)
	}

	date := createField(
		t,
		harness,
		table.GetId(),
		"日期",
		structurev1.FieldType_FIELD_TYPE_DATE,
		map[string]any{"include_time": false, "format": "YYYY-MM-DD"},
	)
	_, err = harness.client.ConvertField(ctx, &structurev1.ConvertFieldRequest{
		FieldId:    date.GetId(),
		TargetType: structurev1.FieldType_FIELD_TYPE_NUMBER,
		Confirm:    true,
	})
	requireErrorCode(t, err, 1001)
	if got := listFieldType(t, harness, table.GetId(), date.GetId()); got != structurev1.FieldType_FIELD_TYPE_DATE {
		t.Fatalf("rejected conversion changed field type to %s", got)
	}
}

func listFieldType(
	t *testing.T,
	harness *structureHarness,
	tableID, fieldID string,
) structurev1.FieldType {
	t.Helper()
	response, err := harness.client.ListFields(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	for _, field := range response.GetFieldList() {
		if field.GetId() == fieldID {
			return field.GetType()
		}
	}
	t.Fatalf("field %s not found", fieldID)
	return structurev1.FieldType_FIELD_TYPE_UNSPECIFIED
}
