package workspace_test

import (
	"strings"
	"testing"

	workspacev1 "excel/api/workspace/v1"
)

func TestDuplicateBaseCopiesStructureAndRecordsOnly(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	source, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "产品计划",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}

	response, err := harness.client.DuplicateBase(ctx, &workspacev1.DuplicateBaseRequest{
		BaseId: source.GetBase().GetId(),
	})
	if err != nil {
		t.Fatalf("DuplicateBase() error = %v", err)
	}
	duplicate := response.GetBase()
	if duplicate.GetId() == source.GetBase().GetId() {
		t.Fatal("DuplicateBase() reused the source Base id")
	}
	if !strings.Contains(duplicate.GetName(), "副本") {
		t.Fatalf("DuplicateBase() name = %q, want 副本 suffix", duplicate.GetName())
	}

	sourceTables, err := harness.structure.TablesByBase(
		authenticatedIncomingContext(t, testOwner, testEnterprise),
		testEnterprise,
		source.GetBase().GetId(),
	)
	if err != nil {
		t.Fatalf("source TablesByBase() error = %v", err)
	}
	duplicateTables, err := harness.structure.TablesByBase(
		authenticatedIncomingContext(t, testOwner, testEnterprise),
		testEnterprise,
		duplicate.GetId(),
	)
	if err != nil {
		t.Fatalf("duplicate TablesByBase() error = %v", err)
	}
	if len(duplicateTables) != len(sourceTables) || len(duplicateTables) == 0 {
		t.Fatalf("duplicate tables = %d, source tables = %d", len(duplicateTables), len(sourceTables))
	}
	for index := range sourceTables {
		if duplicateTables[index].ID == sourceTables[index].ID {
			t.Fatalf("table[%d] reused source id", index)
		}
		if duplicateTables[index].Name != sourceTables[index].Name {
			t.Fatalf("table[%d] name = %q, want %q",
				index, duplicateTables[index].Name, sourceTables[index].Name)
		}
		fields, err := harness.structure.FieldsByTable(
			authenticatedIncomingContext(t, testOwner, testEnterprise),
			duplicateTables[index].ID,
		)
		if err != nil {
			t.Fatalf("duplicate FieldsByTable() error = %v", err)
		}
		if len(fields) != 3 || fields[0].Name != "标题" || !fields[0].IsPrimary {
			t.Fatalf("duplicate fields = %+v", fields)
		}
		views, err := harness.structure.ViewsByTable(
			authenticatedIncomingContext(t, testOwner, testEnterprise),
			duplicateTables[index].ID,
		)
		if err != nil {
			t.Fatalf("duplicate ViewsByTable() error = %v", err)
		}
		if len(views) != 1 || views[0].Name != "表格" {
			t.Fatalf("duplicate views = %+v", views)
		}
	}
	if len(harness.records.calls) != len(sourceTables) {
		t.Fatalf("record duplication calls = %v, want one per table", harness.records.calls)
	}
	if len(harness.comments.baseIDs) != 0 {
		t.Fatalf("duplicate unexpectedly touched comments: %v", harness.comments.baseIDs)
	}
}
