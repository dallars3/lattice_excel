package structure_test

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// TestUpdateOptionsBaselineWriteBackSucceeds 锁定 H-40 契约：对当前 option_list
// 全量回写必须成功（缺陷 #15：曾因 0 行变更误判 + 未映射仓储错误恒报 9001），
// 且选项 id 跨更新保持稳定。
func TestUpdateOptionsBaselineWriteBackSucceeds(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "选项基线t07")
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	field := createField(t, harness, table.GetId(), "状态t07", structurev1.FieldType_FIELD_TYPE_SELECT, map[string]any{
		"option_list": []any{
			map[string]any{"id": "optt07aa", "name": "待办", "color": "blue"},
			map[string]any{"id": "optt07bb", "name": "完成", "color": "green"},
		},
		"allow_create": false,
	})

	response, err := harness.client.UpdateOptions(ctx, &structurev1.UpdateOptionsRequest{
		FieldId: field.GetId(),
		OptionList: []*structurev1.FieldOption{
			{Id: "optt07aa", Name: "待办", Color: "blue"},
			{Id: "optt07bb", Name: "完成", Color: "green"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateOptions(baseline) error = %v", err)
	}
	assertOptionList(t, response.GetField().GetConfigMap(), []map[string]string{
		{"id": "optt07aa", "name": "待办", "color": "blue"},
		{"id": "optt07bb", "name": "完成", "color": "green"},
	})

	fields := mustListFields(t, harness, table.GetId())
	assertOptionList(t, fieldByName(t, fields, "状态t07").GetConfigMap(), []map[string]string{
		{"id": "optt07aa", "name": "待办", "color": "blue"},
		{"id": "optt07bb", "name": "完成", "color": "green"},
	})
}

// TestUpdateOptionsStableIDsForUnnamedOptions：回写不带 id（H-40 契约 id 可省略）
// 时，同名选项必须复用既有 id（记录值引用选项 id，不能因维护改名/调色而失效）；
// 新增选项由服务端生成 id，且再次回写后 id 保持不变。
func TestUpdateOptionsStableIDsForUnnamedOptions(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "选项稳定t07")
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	field := createField(t, harness, table.GetId(), "颜色t07", structurev1.FieldType_FIELD_TYPE_SELECT, map[string]any{
		"option_list": []any{
			map[string]any{"id": "optt07red", "name": "红色", "color": "red"},
			map[string]any{"id": "optt07blue", "name": "蓝色", "color": "blue"},
		},
		"allow_create": true,
	})

	response, err := harness.client.UpdateOptions(ctx, &structurev1.UpdateOptionsRequest{
		FieldId: field.GetId(),
		OptionList: []*structurev1.FieldOption{
			{Name: "红色", Color: "#ff0000"}, // 改色，未带 id
			{Name: "蓝色", Color: "blue"},    // 未带 id
			{Name: "绿色", Color: "green"},   // 新增
		},
		AllowCreate: true,
	})
	if err != nil {
		t.Fatalf("UpdateOptions(unnamed ids) error = %v", err)
	}
	assertOptionList(t, response.GetField().GetConfigMap(), []map[string]string{
		{"id": "optt07red", "name": "红色", "color": "#ff0000"},
		{"id": "optt07blue", "name": "蓝色", "color": "blue"},
		{"id": anyGeneratedID, "name": "绿色", "color": "green"},
	})

	// 二次回写带回生成的 id：id 不变（跨更新稳定）。
	generatedID := optionIDByName(t, response.GetField().GetConfigMap(), "绿色")
	response, err = harness.client.UpdateOptions(ctx, &structurev1.UpdateOptionsRequest{
		FieldId: field.GetId(),
		OptionList: []*structurev1.FieldOption{
			{Id: "optt07red", Name: "红色", Color: "red"},
			{Id: "optt07blue", Name: "蓝色", Color: "blue"},
			{Id: generatedID, Name: "绿色", Color: "green"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateOptions(write back generated ids) error = %v", err)
	}
	assertOptionList(t, response.GetField().GetConfigMap(), []map[string]string{
		{"id": "optt07red", "name": "红色", "color": "red"},
		{"id": "optt07blue", "name": "蓝色", "color": "blue"},
		{"id": generatedID, "name": "绿色", "color": "green"},
	})
}

// TestUpdateOptionsMergeSameNameEntries：同名（去空格、忽略大小写）归一合并，
// 不报重名失败也不新增重复选项（缺陷 #15：曾报 9001/1001）；既有 id 保留。
func TestUpdateOptionsMergeSameNameEntries(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "选项合并t07")
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	field := createField(t, harness, table.GetId(), "标签t07", structurev1.FieldType_FIELD_TYPE_MULTISELECT, map[string]any{
		"option_list": []any{
			map[string]any{"id": "optt07todo", "name": "TODO", "color": "gray"},
		},
		"allow_create": false,
	})

	response, err := harness.client.UpdateOptions(ctx, &structurev1.UpdateOptionsRequest{
		FieldId: field.GetId(),
		OptionList: []*structurev1.FieldOption{
			{Id: "optt07todo", Name: " TODO ", Color: "gray"},
			{Name: "todo", Color: "red"}, // 与上一项去空格忽略大小写后同名
			{Name: "进行中", Color: "blue"},
			{Name: " 进行中 ", Color: "green"}, // 与上一项同名（仅空格差异）
		},
	})
	if err != nil {
		t.Fatalf("UpdateOptions(same-name merge) error = %v", err)
	}
	assertOptionList(t, response.GetField().GetConfigMap(), []map[string]string{
		{"id": "optt07todo", "name": "TODO", "color": "gray"},
		{"id": anyGeneratedID, "name": "进行中", "color": "blue"},
	})
}

// TestUpdateOptionsMissingFieldIsDeletedResource：仓储「字段不存在」映射为契约
// 错误 4002，而不是裸错误呈现 9001。
func TestUpdateOptionsMissingFieldIsDeletedResource(t *testing.T) {
	harness := newStructureHarness(t)
	createTable(t, harness, "选项缺字段t07")
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	_, err := harness.client.UpdateOptions(ctx, &structurev1.UpdateOptionsRequest{
		FieldId: "01JT07MISSING0000000000000",
		OptionList: []*structurev1.FieldOption{
			{Name: "待办", Color: "blue"},
		},
	})
	requireErrorCode(t, err, deal.CodeDeletedResource)
}

// anyGeneratedID 表示「服务端生成、非空且非既有 id」的占位期望。
const anyGeneratedID = "*"

// optionIDByName 从 config_map 的 option_list 按名称取选项 id。
func optionIDByName(t *testing.T, configMap *structpb.Struct, name string) string {
	t.Helper()
	if configMap == nil {
		t.Fatal("config_map is nil")
	}
	value, ok := configMap.GetFields()["option_list"]
	if !ok {
		t.Fatalf("config_map missing option_list: %v", configMap.GetFields())
	}
	for _, option := range value.GetListValue().GetValues() {
		fields := option.GetStructValue().GetFields()
		if fields["name"].GetStringValue() == name {
			return fields["id"].GetStringValue()
		}
	}
	t.Fatalf("option %q not found", name)
	return ""
}

func assertOptionList(
	t *testing.T,
	configMap *structpb.Struct,
	want []map[string]string,
) {
	t.Helper()
	if configMap == nil {
		t.Fatal("config_map is nil")
	}
	value, ok := configMap.GetFields()["option_list"]
	if !ok {
		t.Fatalf("config_map missing option_list: %v", configMap.GetFields())
	}
	options := value.GetListValue().GetValues()
	if len(options) != len(want) {
		t.Fatalf("option count = %d, want %d", len(options), len(want))
	}
	for index, option := range options {
		fields := option.GetStructValue().GetFields()
		id := fields["id"].GetStringValue()
		name := fields["name"].GetStringValue()
		color := fields["color"].GetStringValue()
		wantID := want[index]["id"]
		if wantID == anyGeneratedID {
			if id == "" {
				t.Fatalf("option[%d] (%q) id is empty, want generated id", index, name)
			}
			wantID = id
		}
		if id != wantID || name != want[index]["name"] || color != want[index]["color"] {
			t.Fatalf(
				"option[%d] = {id:%q name:%q color:%q}, want {id:%q name:%q color:%q}",
				index, id, name, color,
				want[index]["id"], want[index]["name"], want[index]["color"],
			)
		}
	}
}
