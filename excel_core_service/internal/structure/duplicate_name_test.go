package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// TestStructureDuplicateNameReturnsConflict 落契约：表/字段/视图/选项重名返回
// 1002（对象重名），参数缺失/非法仍为 1001（缺陷 #14，H-30/H-35/H-42 附录 B）。
func TestStructureDuplicateNameReturnsConflict(t *testing.T) {
	harness := newStructureHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	table := createTable(t, harness, "重名表t07")

	// 表重名。
	_, err := harness.client.CreateTable(ctx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "重名表t07",
	})
	requireErrorCode(t, err, deal.CodeConflict)

	// 字段重名。
	createField(t, harness, table.GetId(), "数量t07", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	_, err = harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: table.GetId(),
		Name:    " 数量t07 ",
		Type:    structurev1.FieldType_FIELD_TYPE_TEXT,
	})
	requireErrorCode(t, err, deal.CodeConflict)

	// 视图重名。
	_, err = harness.client.CreateView(ctx, &structurev1.CreateViewRequest{
		TableId: table.GetId(),
		Name:    "看板t07",
		Type:    "table",
	})
	if err != nil {
		t.Fatalf("CreateView(看板t07) first error = %v", err)
	}
	_, err = harness.client.CreateView(ctx, &structurev1.CreateViewRequest{
		TableId: table.GetId(),
		Name:    "看板t07",
		Type:    "table",
	})
	requireErrorCode(t, err, deal.CodeConflict)

	// 选项重名（同一字段配置内，去空格后重名）。
	_, err = harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: table.GetId(),
		Name:    "选项重名t07",
		Type:    structurev1.FieldType_FIELD_TYPE_SELECT,
		ConfigMap: configMap(t, map[string]any{
			"option_list": []any{
				map[string]any{"name": "甲t07", "color": "blue"},
				map[string]any{"name": " 甲t07 ", "color": "red"},
			},
		}),
	})
	requireErrorCode(t, err, deal.CodeConflict)

	// 重命名到已有名称：表/字段/视图。
	otherTable := createTable(t, harness, "另一表t07")
	_, err = harness.client.RenameTable(ctx, &structurev1.RenameTableRequest{
		TableId: otherTable.GetId(),
		Name:    "重名表t07",
	})
	requireErrorCode(t, err, deal.CodeConflict)

	otherField := createField(
		t, harness, table.GetId(), "另一字段t07", structurev1.FieldType_FIELD_TYPE_TEXT, nil,
	)
	_, err = harness.client.RenameField(ctx, &structurev1.RenameFieldRequest{
		FieldId: otherField.GetId(),
		Name:    "数量t07",
	})
	requireErrorCode(t, err, deal.CodeConflict)

	_, err = harness.client.CreateView(ctx, &structurev1.CreateViewRequest{
		TableId: table.GetId(),
		Name:    "另一视图t07",
		Type:    "table",
	})
	if err != nil {
		t.Fatalf("CreateView(另一视图t07) error = %v", err)
	}
	_, err = harness.client.RenameView(ctx, &structurev1.RenameViewRequest{
		ViewId: viewIDByName(t, harness, table.GetId(), "另一视图t07"),
		Name:   "看板t07",
	})
	requireErrorCode(t, err, deal.CodeConflict)

	// 参数缺失/非法仍为 1001。
	_, err = harness.client.CreateTable(ctx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "   ",
	})
	requireErrorCode(t, err, deal.CodeInvalidArgument)
	_, err = harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: table.GetId(),
		Name:    "坏类型t07",
		Type:    structurev1.FieldType_FIELD_TYPE_UNSPECIFIED,
	})
	requireErrorCode(t, err, deal.CodeInvalidArgument)

	// 重名对象均未落库。
	fields := mustListFields(t, harness, table.GetId())
	if got := len(fields); got != 3 { // 标题 + 数量t07 + 另一字段t07
		t.Fatalf("field count = %d, want 3; fields = %v", got, fieldNames(fields))
	}
	views := mustListViews(t, harness, table.GetId())
	if got := len(views); got != 3 { // 表格 + 看板t07 + 另一视图t07
		t.Fatalf("view count = %d, want 3; views = %v", got, viewNames(views))
	}
}

// TestRenameToOwnNameSucceeds 锁定幂等语义：重命名为当前名称不是重名冲突，
// 也不允许误报「已被删除」（MySQL 0 行变更曾被误判为 4002）。
func TestRenameToOwnNameSucceeds(t *testing.T) {
	harness := newStructureHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	table := createTable(t, harness, "自改名表t07")
	field := createField(t, harness, table.GetId(), "自改字段t07", structurev1.FieldType_FIELD_TYPE_TEXT, nil)
	_, err := harness.client.CreateView(ctx, &structurev1.CreateViewRequest{
		TableId: table.GetId(),
		Name:    "自改视图t07",
		Type:    "table",
	})
	if err != nil {
		t.Fatalf("CreateView(自改视图t07) error = %v", err)
	}

	if _, err := harness.client.RenameTable(ctx, &structurev1.RenameTableRequest{
		TableId: table.GetId(),
		Name:    "自改名表t07",
	}); err != nil {
		t.Fatalf("RenameTable(own name) error = %v", err)
	}
	if _, err := harness.client.RenameField(ctx, &structurev1.RenameFieldRequest{
		FieldId: field.GetId(),
		Name:    "自改字段t07",
	}); err != nil {
		t.Fatalf("RenameField(own name) error = %v", err)
	}
	if _, err := harness.client.RenameView(ctx, &structurev1.RenameViewRequest{
		ViewId: viewIDByName(t, harness, table.GetId(), "自改视图t07"),
		Name:   "自改视图t07",
	}); err != nil {
		t.Fatalf("RenameView(own name) error = %v", err)
	}

	tables, err := harness.client.ListTables(ctx, &structurev1.ListTablesRequest{BaseId: testBaseID})
	if err != nil {
		t.Fatalf("ListTables() error = %v", err)
	}
	found := false
	for _, entry := range tables.GetTableList() {
		if entry.GetId() == table.GetId() && entry.GetName() == "自改名表t07" {
			found = true
		}
	}
	if !found {
		t.Fatal("renamed table missing after own-name rename")
	}
}

func viewIDByName(
	t *testing.T,
	harness *structureHarness,
	tableID, name string,
) string {
	t.Helper()
	views := mustListViews(t, harness, tableID)
	for _, view := range views {
		if view.GetName() == name {
			return view.GetId()
		}
	}
	t.Fatalf("view %q not found", name)
	return ""
}

func viewNames(views []*structurev1.View) []string {
	names := make([]string, 0, len(views))
	for _, view := range views {
		names = append(names, view.GetName())
	}
	return names
}
