package structure_test

import (
	"strings"
	"testing"

	structurev1 "excel/api/structure/v1"
	"google.golang.org/grpc/status"
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

// 票 07（BUG-05 后端）：公式表达式保存期校验——未知函数（calc 注册表无 CONCAT）、
// 未知引用拒绝且文案含原因；合法表达式创建成功；空白沿用现状（配置规范化拒绝）。
// 每个用例独立建表，互不污染（主字段自动创建，拒绝不落库时只有主字段）。
func TestFormulaExpressionValidationAtSave(t *testing.T) {
	harness := newStructureHarness(t)

	cases := []struct {
		name        string
		fieldName   string
		expression  string
		wantCode    int
		wantMessage string // 空串表示不断言文案
	}{
		{
			name:        "unknown function",
			fieldName:   "未知函数公式",
			expression:  `CONCAT({标题}, "-后缀")`,
			wantCode:    1001,
			wantMessage: "未知函数: CONCAT",
		},
		{
			name:        "missing field reference",
			fieldName:   "未知引用公式",
			expression:  "{不存在的字段}+1",
			wantCode:    1001,
			wantMessage: "不存在的字段",
		},
		{
			name:       "blank expression",
			fieldName:  "空白公式",
			expression: "   ",
			wantCode:   1001,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			table := createTable(t, harness, "公式校验表："+test.fieldName)
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
			requireErrorCode(t, err, test.wantCode)
			if test.wantMessage != "" {
				requireErrorMessage(t, err, test.wantMessage)
			}
			list, err := harness.client.ListFields(
				authenticatedContext(t, testOwner, testEnterprise),
				&structurev1.ListFieldsRequest{TableId: table.GetId()},
			)
			if err != nil {
				t.Fatalf("ListFields() error = %v", err)
			}
			if len(list.GetFieldList()) != 1 {
				t.Fatalf("ListFields() = %d fields, want 1 (拒绝不落库，仅主字段)", len(list.GetFieldList()))
			}
		})
	}

	table := createTable(t, harness, "公式校验表：合法")
	createField(
		t,
		harness,
		table.GetId(),
		"数量",
		structurev1.FieldType_FIELD_TYPE_NUMBER,
		nil,
	)
	created := createField(
		t,
		harness,
		table.GetId(),
		"合法公式",
		structurev1.FieldType_FIELD_TYPE_FORMULA,
		map[string]any{"expression": "CONCATENATE({数量}, \"倍\")"},
	)
	config := created.GetConfigMap().AsMap()
	if config["expression"] != `CONCATENATE({数量}, "倍")` {
		t.Fatalf("expression = %v, want CONCATENATE 原样保存", config["expression"])
	}

	// 修改路径（H-37 UpdateFieldConfig）同样校验：改为未知函数拒绝且文案含原因，
	// 原配置不被破坏。
	t.Run("update to unknown function", func(t *testing.T) {
		_, err := harness.client.UpdateFieldConfig(
			authenticatedContext(t, testOwner, testEnterprise),
			&structurev1.UpdateFieldConfigRequest{
				FieldId: created.GetId(),
				ConfigMap: configMap(t, map[string]any{
					"expression": `CONCAT({数量})`,
				}),
			},
		)
		requireErrorCode(t, err, 1001)
		requireErrorMessage(t, err, "未知函数: CONCAT")
		after, err := harness.client.ListFields(
			authenticatedContext(t, testOwner, testEnterprise),
			&structurev1.ListFieldsRequest{TableId: table.GetId()},
		)
		if err != nil {
			t.Fatalf("ListFields() error = %v", err)
		}
		for _, field := range after.GetFieldList() {
			if field.GetId() == created.GetId() &&
				field.GetConfigMap().AsMap()["expression"] != `CONCATENATE({数量}, "倍")` {
				t.Fatalf("rejected update mutated config: %v", field.GetConfigMap().AsMap())
			}
		}
	})
}

// requireErrorMessage 断言 gRPC 错误 message 含原因片段（票 07：文案含原因）。
func requireErrorMessage(t *testing.T, err error, substring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want message contains %q", substring)
	}
	message := status.Convert(err).Message()
	if !strings.Contains(message, substring) {
		t.Fatalf("error message = %q, want contains %q", message, substring)
	}
}
