package structure_test

import (
	"strings"
	"testing"

	structurev1 "excel/api/structure/v1"
)

func TestFieldDescriptionRoundTripAndValidation(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "字段描述")
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	const description = "用于悬停展示的字段说明"

	created, err := harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId:     table.GetId(),
		Name:        "带说明",
		Type:        structurev1.FieldType_FIELD_TYPE_TEXT,
		Description: description,
	})
	if err != nil {
		t.Fatalf("CreateField(desc) error = %v", err)
	}
	if got := created.GetField().GetDescription(); got != description {
		t.Fatalf("CreateField response description = %q, want %q", got, description)
	}

	fields := mustListFields(t, harness, table.GetId())
	if got := fieldByName(t, fields, "带说明").GetDescription(); got != description {
		t.Fatalf("ListFields description = %q, want %q", got, description)
	}

	// 未设描述 → 返回空串，既有调用不受影响。
	blank := createField(t, harness, table.GetId(), "无说明", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	if got := blank.GetDescription(); got != "" {
		t.Fatalf("field without description = %q, want empty", got)
	}
	fields = mustListFields(t, harness, table.GetId())
	if got := fieldByName(t, fields, "无说明").GetDescription(); got != "" {
		t.Fatalf("listed field without description = %q, want empty", got)
	}

	// 超长描述被拒且字段不落库。
	_, err = harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId:     table.GetId(),
		Name:        "超长说明",
		Type:        structurev1.FieldType_FIELD_TYPE_TEXT,
		Description: strings.Repeat("字", 501),
	})
	requireErrorCode(t, err, 1001)

	fields = mustListFields(t, harness, table.GetId())
	for _, field := range fields {
		if field.GetName() == "超长说明" {
			t.Fatalf("overlong description field was persisted")
		}
	}
}
