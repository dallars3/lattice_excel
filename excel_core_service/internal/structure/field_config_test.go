package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
)

func TestFieldConfigValidationAndPersistence(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "任务")
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	name := createField(t, harness, table.GetId(), "名称", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	relation := createField(
		t,
		harness,
		table.GetId(),
		"关联",
		structurev1.FieldType_FIELD_TYPE_RELATION,
		map[string]any{"target_table_id": table.GetId(), "allow_multiple": false},
	)
	createField(
		t,
		harness,
		table.GetId(),
		"引用",
		structurev1.FieldType_FIELD_TYPE_LOOKUP,
		map[string]any{
			"relation_field_id": relation.GetId(),
			"target_field_id":   name.GetId(),
		},
	)

	legalCases := []struct {
		name   string
		field  structurev1.FieldType
		config map[string]any
	}{
		{"文本", structurev1.FieldType_FIELD_TYPE_TEXT, nil},
		{
			"数字",
			structurev1.FieldType_FIELD_TYPE_NUMBER,
			map[string]any{
				"format": "decimal", "decimals": 4, "currency": "¥", "thousands": true,
			},
		},
		{
			"单选",
			structurev1.FieldType_FIELD_TYPE_SELECT,
			map[string]any{
				"option_list":  []any{map[string]any{"id": "opt_a", "name": "甲", "color": "blue"}},
				"allow_create": true,
			},
		},
		{
			"多选",
			structurev1.FieldType_FIELD_TYPE_MULTISELECT,
			map[string]any{
				"option_list":  []any{map[string]any{"name": "乙", "color": "green"}},
				"allow_create": false,
			},
		},
		{
			"日期",
			structurev1.FieldType_FIELD_TYPE_DATE,
			map[string]any{"include_time": true, "format": "YYYY-MM-DD"},
		},
		{
			"人员",
			structurev1.FieldType_FIELD_TYPE_PERSON,
			map[string]any{"allow_multiple": true},
		},
		{"附件", structurev1.FieldType_FIELD_TYPE_ATTACHMENT, nil},
		{"链接", structurev1.FieldType_FIELD_TYPE_URL, nil},
		{
			"公式",
			structurev1.FieldType_FIELD_TYPE_FORMULA,
			map[string]any{"expression": "{名称}"},
		},
		{
			"合法关联",
			structurev1.FieldType_FIELD_TYPE_RELATION,
			map[string]any{"target_table_id": table.GetId(), "allow_multiple": true},
		},
		{
			"合法引用",
			structurev1.FieldType_FIELD_TYPE_LOOKUP,
			map[string]any{
				"relation_field_id": relation.GetId(),
				"target_field_id":   name.GetId(),
			},
		},
	}
	for _, tc := range legalCases {
		t.Run("legal_"+tc.name, func(t *testing.T) {
			createField(t, harness, table.GetId(), tc.name, tc.field, tc.config)
		})
	}

	invalidCases := []struct {
		name   string
		field  structurev1.FieldType
		config map[string]any
	}{
		{"坏文本", structurev1.FieldType_FIELD_TYPE_TEXT, map[string]any{"unknown": true}},
		{
			"坏小数位",
			structurev1.FieldType_FIELD_TYPE_NUMBER,
			map[string]any{"decimals": 5},
		},
		{
			"坏数字格式",
			structurev1.FieldType_FIELD_TYPE_NUMBER,
			map[string]any{"format": "scientific"},
		},
		{
			"坏选项重复",
			structurev1.FieldType_FIELD_TYPE_SELECT,
			map[string]any{
				"option_list": []any{
					map[string]any{"name": "重复", "color": "blue"},
					map[string]any{"name": " 重复 ", "color": "red"},
				},
			},
		},
		{
			"坏日期格式",
			structurev1.FieldType_FIELD_TYPE_DATE,
			map[string]any{"format": "DD/MM/YYYY"},
		},
		{
			"坏人员开关",
			structurev1.FieldType_FIELD_TYPE_PERSON,
			map[string]any{"allow_multiple": "true"},
		},
		{
			"坏公式引用",
			structurev1.FieldType_FIELD_TYPE_FORMULA,
			map[string]any{"expression": "{不存在}"},
		},
		{
			"坏关联目标",
			structurev1.FieldType_FIELD_TYPE_RELATION,
			map[string]any{"target_table_id": "01J00000000000000000009999"},
		},
		{
			"坏引用关系",
			structurev1.FieldType_FIELD_TYPE_LOOKUP,
			map[string]any{
				"relation_field_id": name.GetId(),
				"target_field_id":   name.GetId(),
			},
		},
		{"坏附件配置", structurev1.FieldType_FIELD_TYPE_ATTACHMENT, map[string]any{"limit": 3}},
		{"坏链接配置", structurev1.FieldType_FIELD_TYPE_URL, map[string]any{"prefix": "ftp"}},
	}
	for _, tc := range invalidCases {
		t.Run("invalid_"+tc.name, func(t *testing.T) {
			_, err := harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
				TableId:   table.GetId(),
				Name:      tc.name,
				Type:      tc.field,
				ConfigMap: configMap(t, tc.config),
			})
			requireErrorCode(t, err, 1001)
		})
	}

	response, err := harness.client.ListFields(ctx, &structurev1.ListFieldsRequest{
		TableId: table.GetId(),
	})
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	if len(response.GetFieldList()) != len(legalCases)+3 {
		t.Fatalf("field count = %d, want %d; fields = %v",
			len(response.GetFieldList()), len(legalCases)+3, fieldNames(response.GetFieldList()))
	}
	for _, tc := range invalidCases {
		for _, field := range response.GetFieldList() {
			if field.GetName() == tc.name {
				t.Fatalf("invalid field %q was persisted", tc.name)
			}
		}
	}
	number := fieldByName(t, response.GetFieldList(), "数字")
	if number.GetConfigMap().GetFields()["decimals"].GetNumberValue() != 4 {
		t.Fatalf("number decimals = %v, want 4", number.GetConfigMap().GetFields()["decimals"])
	}
}

func TestCrossTableFormulaCycleIsRejectedWithoutPersistence(t *testing.T) {
	harness := newStructureHarness(t)
	tableA := createTable(t, harness, "表 A")
	tableB := createTable(t, harness, "表 B")
	createField(t, harness, tableA.GetId(), "A 主字段", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	createField(t, harness, tableB.GetId(), "B 主字段", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	relationA := createField(
		t,
		harness,
		tableA.GetId(),
		"A 关联",
		structurev1.FieldType_FIELD_TYPE_RELATION,
		map[string]any{"target_table_id": tableB.GetId()},
	)
	relationB := createField(
		t,
		harness,
		tableB.GetId(),
		"B 关联",
		structurev1.FieldType_FIELD_TYPE_RELATION,
		map[string]any{"target_table_id": tableA.GetId()},
	)
	formulaA := createField(
		t,
		harness,
		tableA.GetId(),
		"A 公式",
		structurev1.FieldType_FIELD_TYPE_FORMULA,
		map[string]any{"expression": "{A 主字段}"},
	)
	lookupB := createField(
		t,
		harness,
		tableB.GetId(),
		"B 引用",
		structurev1.FieldType_FIELD_TYPE_LOOKUP,
		map[string]any{
			"relation_field_id": relationB.GetId(),
			"target_field_id":   formulaA.GetId(),
		},
	)
	createField(
		t,
		harness,
		tableA.GetId(),
		"A 引用",
		structurev1.FieldType_FIELD_TYPE_LOOKUP,
		map[string]any{
			"relation_field_id": relationA.GetId(),
			"target_field_id":   lookupB.GetId(),
		},
	)

	_, err := harness.client.UpdateFieldConfig(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.UpdateFieldConfigRequest{
			FieldId:   formulaA.GetId(),
			ConfigMap: configMap(t, map[string]any{"expression": "{A 引用}"}),
		},
	)
	requireErrorCode(t, err, 1001)
	fields, err := harness.client.ListFields(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListFieldsRequest{TableId: tableA.GetId()},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	persisted := fieldByName(t, fields.GetFieldList(), "A 公式")
	if got := persisted.GetConfigMap().GetFields()["expression"].GetStringValue(); got != "{A 主字段}" {
		t.Fatalf("persisted expression = %q, want %q", got, "{A 主字段}")
	}
}

func createField(
	t *testing.T,
	harness *structureHarness,
	tableID, name string,
	fieldType structurev1.FieldType,
	config map[string]any,
) *structurev1.Field {
	t.Helper()
	response, err := harness.client.CreateField(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateFieldRequest{
			TableId:   tableID,
			Name:      name,
			Type:      fieldType,
			ConfigMap: configMap(t, config),
		},
	)
	if err != nil {
		t.Fatalf("CreateField(%q) error = %v", name, err)
	}
	return response.GetField()
}
