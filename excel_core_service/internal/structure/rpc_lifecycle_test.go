package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// TestExternalAccountReadsStructureWithinBaseTenancy 锁「分享链接访客」的只读边界：
// 无企业归属的外部账号不要求企业成员角色，租户边界取数据表所属 Base 的企业；
// 写接口仍要求企业成员，不放宽。
func TestExternalAccountReadsStructureWithinBaseTenancy(t *testing.T) {
	harness := newStructureHarness(t)
	externalCtx := authenticatedContext(t, "01J00000000000000000000009", "")
	table := createTable(t, harness, "外部可读表")
	createField(
		t,
		harness,
		table.GetId(),
		"备注",
		structurev1.FieldType_FIELD_TYPE_TEXT,
		nil,
	)
	views := mustListViews(t, harness, table.GetId())
	if len(views) == 0 {
		t.Fatal("seed table has no default view")
	}

	tables, err := harness.client.ListTables(
		externalCtx,
		&structurev1.ListTablesRequest{BaseId: testBaseID},
	)
	if err != nil {
		t.Fatalf("external ListTables() error = %v", err)
	}
	if len(tables.GetTableList()) != 1 || tables.GetTableList()[0].GetId() != table.GetId() {
		t.Fatalf("external ListTables() = %v, want the Base table", tables.GetTableList())
	}

	fields, err := harness.client.ListFields(
		externalCtx,
		&structurev1.ListFieldsRequest{TableId: table.GetId()},
	)
	if err != nil {
		t.Fatalf("external ListFields() error = %v", err)
	}
	if len(fields.GetFieldList()) != 2 {
		t.Fatalf("external ListFields() = %v, want two fields", fields.GetFieldList())
	}

	listedViews, err := harness.client.ListViews(
		externalCtx,
		&structurev1.ListViewsRequest{TableId: table.GetId()},
	)
	if err != nil {
		t.Fatalf("external ListViews() error = %v", err)
	}
	if len(listedViews.GetViewList()) != len(views) {
		t.Fatalf("external ListViews() = %v, want %d views", listedViews.GetViewList(), len(views))
	}

	if _, err := harness.client.GetViewConfig(
		externalCtx,
		&structurev1.GetViewConfigRequest{ViewId: views[0].GetId()},
	); err != nil {
		t.Fatalf("external GetViewConfig() error = %v", err)
	}

	_, err = harness.client.CreateTable(externalCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "外部不能建表",
	})
	requireErrorCode(t, err, 3001)
	_, err = harness.client.CreateField(externalCtx, &structurev1.CreateFieldRequest{
		TableId: table.GetId(),
		Name:    "外部不能建字段",
		Type:    structurev1.FieldType_FIELD_TYPE_TEXT,
	})
	requireErrorCode(t, err, 3001)
	_, err = harness.client.DeleteTable(externalCtx, &structurev1.DeleteTableRequest{
		TableId: table.GetId(),
		Confirm: true,
	})
	requireErrorCode(t, err, 3001)
}

func TestStructureLifecycleAndRoleBoundaries(t *testing.T) {
	harness := newStructureHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	memberCtx := authenticatedContext(t, testMember, testEnterprise)

	table := createTable(t, harness, "原始表")
	field := createField(
		t,
		harness,
		table.GetId(),
		"原始字段",
		structurev1.FieldType_FIELD_TYPE_TEXT,
		nil,
	)
	if _, err := harness.client.CreateView(ownerCtx, &structurev1.CreateViewRequest{
		TableId: table.GetId(),
		Name:    "默认视图",
		Type:    "table",
	}); err != nil {
		t.Fatalf("CreateView() error = %v", err)
	}

	renamed, err := harness.client.RenameTable(ownerCtx, &structurev1.RenameTableRequest{
		TableId: table.GetId(),
		Name:    "  重命名表  ",
	})
	if err != nil {
		t.Fatalf("RenameTable() error = %v", err)
	}
	if renamed.GetTable().GetName() != "重命名表" {
		t.Fatalf("renamed table = %q", renamed.GetTable().GetName())
	}
	renamedField, err := harness.client.RenameField(ownerCtx, &structurev1.RenameFieldRequest{
		FieldId: field.GetId(),
		Name:    "  重命名字段  ",
	})
	if err != nil {
		t.Fatalf("RenameField() error = %v", err)
	}
	if renamedField.GetField().GetName() != "重命名字段" {
		t.Fatalf("renamed field = %q", renamedField.GetField().GetName())
	}

	duplicatedField, err := harness.client.DuplicateField(ownerCtx, &structurev1.DuplicateFieldRequest{
		FieldId: field.GetId(),
	})
	if err != nil {
		t.Fatalf("DuplicateField() error = %v", err)
	}
	if duplicatedField.GetField().GetIsPrimary() {
		t.Fatal("duplicated primary field must not become another primary")
	}
	if _, err := harness.client.DeleteField(ownerCtx, &structurev1.DeleteFieldRequest{
		FieldId: duplicatedField.GetField().GetId(),
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteField() error = %v", err)
	}

	memberView, err := harness.client.CreateView(memberCtx, &structurev1.CreateViewRequest{
		TableId: table.GetId(),
		Name:    "成员视图",
		Type:    "table",
	})
	if err != nil {
		t.Fatalf("member CreateView() error = %v", err)
	}
	if _, err := harness.client.RenameView(memberCtx, &structurev1.RenameViewRequest{
		ViewId: memberView.GetView().GetId(),
		Name:   "成员视图改名",
	}); err != nil {
		t.Fatalf("member RenameView() error = %v", err)
	}
	if _, err := harness.client.DuplicateView(memberCtx, &structurev1.DuplicateViewRequest{
		ViewId: memberView.GetView().GetId(),
	}); err != nil {
		t.Fatalf("member DuplicateView() error = %v", err)
	}

	_, err = harness.client.CreateTable(memberCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "成员不能建表",
	})
	requireErrorCode(t, err, 3001)
	_, err = harness.client.CreateField(memberCtx, &structurev1.CreateFieldRequest{
		TableId: table.GetId(),
		Name:    "成员不能建字段",
		Type:    structurev1.FieldType_FIELD_TYPE_TEXT,
	})
	requireErrorCode(t, err, 3001)

	duplicatedTable, err := harness.client.DuplicateTable(ownerCtx, &structurev1.DuplicateTableRequest{
		TableId: table.GetId(),
	})
	if err != nil {
		t.Fatalf("DuplicateTable() error = %v", err)
	}
	copiedFields := mustListFields(t, harness, duplicatedTable.GetTable().GetId())
	// 源表自带默认主字段「标题」+ 新建并改名的「重命名字段」。
	if len(copiedFields) != 2 || copiedFields[0].GetName() != "标题" ||
		!copiedFields[0].GetIsPrimary() || copiedFields[1].GetName() != "重命名字段" {
		t.Fatalf("copied fields = %v", copiedFields)
	}
	copiedViews := mustListViews(t, harness, duplicatedTable.GetTable().GetId())
	// 源表视图：默认「表格」+ 默认视图 + 成员视图 + 成员视图副本。
	if len(copiedViews) != 4 {
		t.Fatalf("copied view count = %d, want 4", len(copiedViews))
	}
	if _, err := harness.client.DeleteTable(ownerCtx, &structurev1.DeleteTableRequest{
		TableId: duplicatedTable.GetTable().GetId(),
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteTable() error = %v", err)
	}
	_, err = harness.client.DeleteTable(ownerCtx, &structurev1.DeleteTableRequest{
		TableId: table.GetId(),
		Confirm: true,
	})
	// H-33（票 04）：删最后一张表返回专有业务码 4003，不再落入 1001「参数缺失或非法」。
	requireErrorCode(t, err, deal.CodeLastTableProtected)
}
