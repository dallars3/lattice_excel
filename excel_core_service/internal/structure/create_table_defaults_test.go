package structure_test

import (
	"context"
	"testing"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

// 新建数据表必须自带默认主字段与默认视图（规格 §9）：口径与前端兜底一致。
func TestCreateTableCreatesDefaultPrimaryFieldAndView(t *testing.T) {
	harness := newStructureHarness(t)
	table := createTable(t, harness, "默认结构")
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	if table.GetFieldCount() != 1 || table.GetViewCount() != 1 {
		t.Fatalf("created table counts = (%d,%d), want (1,1)",
			table.GetFieldCount(), table.GetViewCount())
	}

	fields := mustListFields(t, harness, table.GetId())
	if len(fields) != 1 {
		t.Fatalf("field count = %d, want 1 (%v)", len(fields), fieldNames(fields))
	}
	primary := fields[0]
	if primary.GetName() != "标题" || !primary.GetIsPrimary() ||
		primary.GetType() != structurev1.FieldType_FIELD_TYPE_TEXT ||
		primary.GetPosition() != 0 || len(primary.GetConfigMap().GetFields()) != 0 {
		t.Fatalf("default primary field = %v", primary)
	}

	views := mustListViews(t, harness, table.GetId())
	if len(views) != 1 {
		t.Fatalf("view count = %d, want 1", len(views))
	}
	if views[0].GetName() != "表格" || views[0].GetType() != "table" {
		t.Fatalf("default view = %v", views[0])
	}
	config, err := harness.client.GetViewConfig(ctx, &structurev1.GetViewConfigRequest{
		ViewId: views[0].GetId(),
	})
	if err != nil {
		t.Fatalf("GetViewConfig() error = %v", err)
	}
	if len(config.GetViewConfig().GetFilterList()) != 0 ||
		len(config.GetViewConfig().GetSortList()) != 0 ||
		len(config.GetViewConfig().GetGroupByList()) != 0 ||
		len(config.GetViewConfig().GetHiddenFieldList()) != 0 {
		t.Fatalf("default view config = %v, want empty", config.GetViewConfig())
	}

	for _, listed := range mustListTables(t, harness) {
		if listed.GetId() != table.GetId() {
			continue
		}
		if listed.GetFieldCount() != 1 || listed.GetViewCount() != 1 {
			t.Fatalf("listed table counts = (%d,%d), want (1,1)",
				listed.GetFieldCount(), listed.GetViewCount())
		}
		return
	}
	t.Fatalf("created table %s missing from ListTables", table.GetId())
}

// 默认结构任一步失败时整笔回滚：不留下无字段/视图的半张表。
func TestCreateTableWithDefaultsRollsBackWhenDefaultViewInsertFails(t *testing.T) {
	harness := newStructureHarness(t)
	createTable(t, harness, "已有表")
	existingTables, err := base.NewStructureRepository(harness.db).TablesByBase(
		context.Background(),
		testEnterprise,
		testBaseID,
	)
	if err != nil {
		t.Fatalf("TablesByBase() error = %v", err)
	}
	if len(existingTables) != 1 {
		t.Fatalf("existing table count = %d, want 1", len(existingTables))
	}
	existingViews := mustListViews(t, harness, existingTables[0].ID)
	if len(existingViews) != 1 {
		t.Fatalf("existing view count = %d, want 1", len(existingViews))
	}

	repository := base.NewStructureRepository(harness.db)
	now := harness.now.UnixMilli()
	const tableID = "tttttttttttttttttttttttttt"
	config := []byte("{}")
	err = repository.CreateTableWithDefaults(
		context.Background(),
		base.StructureTable{
			ID:           tableID,
			EnterpriseID: testEnterprise,
			BaseID:       testBaseID,
			Name:         "回滚表",
			Position:     1,
			CreatedAt:    now,
		},
		base.StructureField{
			ID:        "ffffffffffffffffffffffffff",
			Name:      "标题",
			Type:      "FIELD_TYPE_TEXT",
			Config:    config,
			IsPrimary: true,
			CreatedAt: now,
		},
		base.StructureView{
			// 复用已有视图主键，让默认视图插入失败（前两步已写入事务）。
			ID:        existingViews[0].GetId(),
			Name:      "表格",
			Type:      "table",
			Config:    config,
			CreatedAt: now,
		},
	)
	if err == nil {
		t.Fatal("CreateTableWithDefaults() error = nil, want duplicate view key failure")
	}

	tables, err := repository.TablesByBase(context.Background(), testEnterprise, testBaseID)
	if err != nil {
		t.Fatalf("TablesByBase() after failure error = %v", err)
	}
	for _, table := range tables {
		if table.ID == tableID {
			t.Fatalf("half-created table %s survived rollback", tableID)
		}
	}
	fields, err := repository.FieldsByTable(context.Background(), tableID)
	if err != nil {
		t.Fatalf("FieldsByTable() after failure error = %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("half-created fields survived rollback: %d", len(fields))
	}
}
