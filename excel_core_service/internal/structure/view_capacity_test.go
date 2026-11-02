package structure_test

import (
	"fmt"
	"testing"

	structurev1 "excel/api/structure/v1"
)

func TestViewConfigLimitsPersistenceNamesAndCapacity(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "视图表")
	primary := createField(
		t,
		harness,
		table.GetId(),
		"主字段",
		structurev1.FieldType_FIELD_TYPE_TEXT,
		nil,
	)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	view := createView(t, harness, table.GetId(), "表格")
	createView(t, harness, table.GetId(), "次要视图")
	_, err := harness.client.CreateView(ctx, &structurev1.CreateViewRequest{
		TableId: table.GetId(),
		Name:    " 表格 ",
		Type:    "table",
	})
	requireErrorCode(t, err, 1001)

	_, err = harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: view.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			FilterList: repeatedFilters(primary.GetId(), 6),
		},
	})
	requireErrorCode(t, err, 1001)

	_, err = harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: view.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			SortList: repeatedSorts(primary.GetId(), 4),
		},
	})
	requireErrorCode(t, err, 1001)

	_, err = harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId: view.GetId(),
		ViewConfig: &structurev1.ViewConfig{
			GroupByList: []*structurev1.ViewGroup{
				{FieldId: primary.GetId(), Granularity: structurev1.Granularity_GRANULARITY_MONTH},
				{FieldId: primary.GetId(), Granularity: structurev1.Granularity_GRANULARITY_DAY},
			},
		},
	})
	requireErrorCode(t, err, 1001)

	valid := &structurev1.ViewConfig{
		FilterList: repeatedFilters(primary.GetId(), 5),
		SortList:   repeatedSorts(primary.GetId(), 3),
		GroupByList: []*structurev1.ViewGroup{{
			FieldId:     primary.GetId(),
			Granularity: structurev1.Granularity_GRANULARITY_DAY,
		}},
		RowHeight: structurev1.RowHeight_ROW_HEIGHT_TALL,
	}
	if _, err := harness.client.UpdateViewConfig(ctx, &structurev1.UpdateViewConfigRequest{
		ViewId:     view.GetId(),
		ViewConfig: valid,
	}); err != nil {
		t.Fatalf("UpdateViewConfig(valid) error = %v", err)
	}
	persisted, err := harness.client.GetViewConfig(ctx, &structurev1.GetViewConfigRequest{
		ViewId: view.GetId(),
	})
	if err != nil {
		t.Fatalf("GetViewConfig() error = %v", err)
	}
	if len(persisted.GetViewConfig().GetFilterList()) != 5 ||
		len(persisted.GetViewConfig().GetSortList()) != 3 ||
		len(persisted.GetViewConfig().GetGroupByList()) != 1 {
		t.Fatalf("persisted view config counts = (%d,%d,%d), want (5,3,1)",
			len(persisted.GetViewConfig().GetFilterList()),
			len(persisted.GetViewConfig().GetSortList()),
			len(persisted.GetViewConfig().GetGroupByList()),
		)
	}

	selectField := createField(
		t,
		harness,
		table.GetId(),
		"选项字段",
		structurev1.FieldType_FIELD_TYPE_SELECT,
		map[string]any{
			"option_list": []any{map[string]any{"name": "甲", "color": "blue"}},
		},
	)
	_, err = harness.client.UpdateOptions(ctx, &structurev1.UpdateOptionsRequest{
		FieldId: selectField.GetId(),
		OptionList: []*structurev1.FieldOption{
			{Id: "opt_a", Name: "甲", Color: "blue"},
			{Id: "opt_b", Name: " 甲 ", Color: "red"},
		},
	})
	requireErrorCode(t, err, 1001)

	_, err = harness.client.CreateTable(ctx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   " 视图表 ",
	})
	requireErrorCode(t, err, 1001)
	_, err = harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: table.GetId(),
		Name:    " 主字段 ",
		Type:    structurev1.FieldType_FIELD_TYPE_TEXT,
	})
	requireErrorCode(t, err, 1001)

	tableCount := len(mustListTables(t, harness))
	for index := tableCount; index < 50; index++ {
		createTable(t, harness, fmt.Sprintf("容量表 %02d", index))
	}
	_, err = harness.client.CreateTable(ctx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "超过表上限",
	})
	requireErrorCode(t, err, 5001)

	fieldCount := len(mustListFields(t, harness, table.GetId()))
	for index := fieldCount; index < 100; index++ {
		createField(
			t,
			harness,
			table.GetId(),
			fmt.Sprintf("容量字段 %03d", index),
			structurev1.FieldType_FIELD_TYPE_TEXT,
			nil,
		)
	}
	_, err = harness.client.CreateField(ctx, &structurev1.CreateFieldRequest{
		TableId: table.GetId(),
		Name:    "超过字段上限",
		Type:    structurev1.FieldType_FIELD_TYPE_TEXT,
	})
	requireErrorCode(t, err, 5001)

	views := mustListViews(t, harness, table.GetId())
	if len(views) < 2 {
		t.Fatalf("view count = %d, want at least 2", len(views))
	}
	if _, err := harness.client.DeleteView(ctx, &structurev1.DeleteViewRequest{
		ViewId: views[1].GetId(),
	}); err != nil {
		t.Fatalf("DeleteView() error = %v", err)
	}
	views = mustListViews(t, harness, table.GetId())
	for len(views) > 1 {
		if _, err := harness.client.DeleteView(ctx, &structurev1.DeleteViewRequest{
			ViewId: views[1].GetId(),
		}); err != nil {
			t.Fatalf("DeleteView(keep one) error = %v", err)
		}
		views = mustListViews(t, harness, table.GetId())
	}
	_, err = harness.client.DeleteView(ctx, &structurev1.DeleteViewRequest{
		ViewId: views[0].GetId(),
	})
	requireErrorCode(t, err, 1001)
}

func repeatedFilters(fieldID string, count int) []*structurev1.ViewFilter {
	filters := make([]*structurev1.ViewFilter, 0, count)
	for index := 0; index < count; index++ {
		filters = append(filters, &structurev1.ViewFilter{
			FieldId: fieldID,
			Op:      structurev1.ViewFilterOp_VIEW_FILTER_OP_NOT_EMPTY,
		})
	}
	return filters
}

func repeatedSorts(fieldID string, count int) []*structurev1.ViewSort {
	sorts := make([]*structurev1.ViewSort, 0, count)
	for index := 0; index < count; index++ {
		sorts = append(sorts, &structurev1.ViewSort{
			FieldId: fieldID,
			Dir:     structurev1.SortDir_SORT_DIR_ASC,
		})
	}
	return sorts
}

func createView(
	t *testing.T,
	harness *structureHarness,
	tableID, name string,
) *structurev1.View {
	t.Helper()
	response, err := harness.client.CreateView(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.CreateViewRequest{TableId: tableID, Name: name, Type: "table"},
	)
	if err != nil {
		t.Fatalf("CreateView(%q) error = %v", name, err)
	}
	return response.GetView()
}

func mustListTables(t *testing.T, harness *structureHarness) []*structurev1.Table {
	t.Helper()
	response, err := harness.client.ListTables(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListTablesRequest{BaseId: testBaseID},
	)
	if err != nil {
		t.Fatalf("ListTables() error = %v", err)
	}
	return response.GetTableList()
}

func mustListFields(
	t *testing.T,
	harness *structureHarness,
	tableID string,
) []*structurev1.Field {
	t.Helper()
	response, err := harness.client.ListFields(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListFieldsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	return response.GetFieldList()
}

func mustListViews(
	t *testing.T,
	harness *structureHarness,
	tableID string,
) []*structurev1.View {
	t.Helper()
	response, err := harness.client.ListViews(
		authenticatedContext(t, testOwner, testEnterprise),
		&structurev1.ListViewsRequest{TableId: tableID},
	)
	if err != nil {
		t.Fatalf("ListViews() error = %v", err)
	}
	return response.GetViewList()
}
