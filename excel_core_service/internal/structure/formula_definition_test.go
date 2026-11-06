package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
)

// 工单 09 验收三：公式错误的哨兵化只适用于「求值阶段」（除零/空值/运行期操作
// 数不兼容落 #ERROR:<原因>）。定义类错误仍在保存时拒绝——自引用（环）与引用
// 不存在字段返回 1001，且合法公式不受影响（保存校验不因哨兵化放宽）。
func TestFormulaDefinitionSaveRejectsSelfReferenceAndMissingField(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "哨兵定义表")
	createField(
		t,
		harness,
		table.GetId(),
		"数量",
		structurev1.FieldType_FIELD_TYPE_NUMBER,
		nil,
	)

	cases := []struct {
		name       string
		fieldName  string
		expression string
	}{
		{name: "self reference", fieldName: "自引用公式", expression: "{自引用公式}+1"},
		{name: "missing field", fieldName: "缺失引用公式", expression: "{不存在的字段}+1"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := harness.client.CreateField(
				authenticatedContext(t, testOwner, testEnterprise),
				&structurev1.CreateFieldRequest{
					TableId: table.GetId(),
					Name:    test.fieldName,
					Type:    structurev1.FieldType_FIELD_TYPE_FORMULA,
					ConfigMap: configMap(t, map[string]any{
						"expression": test.expression,
					}),
				},
			)
			requireErrorCode(t, err, 1001)
		})
	}

	createField(
		t,
		harness,
		table.GetId(),
		"合法公式",
		structurev1.FieldType_FIELD_TYPE_FORMULA,
		map[string]any{"expression": "{数量}*2"},
	)
	fields, err := harness.client.ListFields(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListFieldsRequest{TableId: table.GetId()},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	if len(fields.GetFieldList()) != 3 {
		t.Fatalf("ListFields() = %d fields, want 3 (数量 + 合法公式 + 主字段)", len(fields.GetFieldList()))
	}
}
