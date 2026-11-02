package structure_test

import (
	"testing"

	structurev1 "excel/api/structure/v1"
)

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
	if len(copiedFields) != 1 || copiedFields[0].GetName() != "重命名字段" ||
		!copiedFields[0].GetIsPrimary() {
		t.Fatalf("copied fields = %v", copiedFields)
	}
	copiedViews := mustListViews(t, harness, duplicatedTable.GetTable().GetId())
	if len(copiedViews) != 3 {
		t.Fatalf("copied view count = %d, want 3", len(copiedViews))
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
	requireErrorCode(t, err, 1001)
}
