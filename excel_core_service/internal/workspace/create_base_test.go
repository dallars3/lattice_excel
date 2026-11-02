package workspace_test

import (
	"context"
	"testing"

	workspacev1 "excel/api/workspace/v1"
)

func TestCreateBaseCreatesDefaultStructure(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)

	response, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	created := response.GetBase()
	if created.GetName() != "未命名多维表格" {
		t.Fatalf("CreateBase() name = %q", created.GetName())
	}
	if created.GetTableCount() != 1 {
		t.Fatalf("CreateBase() table_count = %d, want 1", created.GetTableCount())
	}

	tables, err := harness.structure.TablesByBase(
		context.Background(),
		testEnterprise,
		created.GetId(),
	)
	if err != nil {
		t.Fatalf("TablesByBase() error = %v", err)
	}
	if len(tables) != 1 || tables[0].Name != "表格" {
		t.Fatalf("default tables = %+v, want one 表格", tables)
	}
	fields, err := harness.structure.FieldsByTable(
		context.Background(),
		tables[0].ID,
	)
	if err != nil {
		t.Fatalf("FieldsByTable() error = %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("default field count = %d, want 3", len(fields))
	}
	wantNames := []string{"标题", "文本", "备注"}
	for index, want := range wantNames {
		if fields[index].Name != want ||
			fields[index].Type != "FIELD_TYPE_TEXT" ||
			fields[index].Position != index {
			t.Fatalf("field[%d] = %+v, want text field %q at position %d",
				index, fields[index], want, index)
		}
		if fields[index].IsPrimary != (index == 0) {
			t.Fatalf("field[%d].IsPrimary = %v", index, fields[index].IsPrimary)
		}
	}
	views, err := harness.structure.ViewsByTable(context.Background(), tables[0].ID)
	if err != nil {
		t.Fatalf("ViewsByTable() error = %v", err)
	}
	if len(views) != 1 || views[0].Name != "表格" || views[0].Type != "table" {
		t.Fatalf("default views = %+v, want one table view", views)
	}
}
