package structure_test

import (
	"context"
	"encoding/json"
	"testing"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// 缺陷 #18（W-11）：字段/表/视图结构写成功后必须把 record.changed 结构事件落
// outbox（realtime 消费转 structure_changed 广播并进入断线重放）。断言持久
// outbox 行而非内存转发：其它测试包的 forwarder 会并发认领共享 outbox。
// 本文件只断言字段写路径；表/视图写路径见同目录表/视图事件测试。
func TestCreateFieldPublishesStructureChangedEvent(t *testing.T) {
	harness := newStructureHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	cleanupStructureChangedOutbox(t, harness)
	t.Cleanup(func() { cleanupStructureChangedOutbox(t, harness) })

	table, err := harness.client.CreateTable(ownerCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "字段事件表",
	})
	if err != nil {
		t.Fatalf("CreateTable error = %v", err)
	}
	tableID := table.GetTable().GetId()

	created, err := harness.client.CreateField(ownerCtx, &structurev1.CreateFieldRequest{
		TableId: tableID,
		Name:    "数量",
		Type:    structurev1.FieldType_FIELD_TYPE_NUMBER,
	})
	if err != nil {
		t.Fatalf("CreateField error = %v", err)
	}
	fieldID := created.GetField().GetId()
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "field_created",
		tableID:    tableID,
		fieldID:    fieldID,
		operator:   testOwner,
	})
	assertStructureChangedRowIDsIncrease(t, harness, testBaseID)
}

func TestStructureFieldWritesPublishStructureChangedEvents(t *testing.T) {
	harness := newStructureHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	cleanupStructureChangedOutbox(t, harness)
	t.Cleanup(func() { cleanupStructureChangedOutbox(t, harness) })

	table, err := harness.client.CreateTable(ownerCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "字段事件表二",
	})
	if err != nil {
		t.Fatalf("CreateTable error = %v", err)
	}
	tableID := table.GetTable().GetId()
	number, err := harness.client.CreateField(ownerCtx, &structurev1.CreateFieldRequest{
		TableId: tableID,
		Name:    "数量",
		Type:    structurev1.FieldType_FIELD_TYPE_NUMBER,
	})
	if err != nil {
		t.Fatalf("CreateField(number) error = %v", err)
	}
	numberID := number.GetField().GetId()
	text, err := harness.client.CreateField(ownerCtx, &structurev1.CreateFieldRequest{
		TableId: tableID,
		Name:    "备注",
		Type:    structurev1.FieldType_FIELD_TYPE_TEXT,
	})
	if err != nil {
		t.Fatalf("CreateField(text) error = %v", err)
	}
	textID := text.GetField().GetId()
	selectField, err := harness.client.CreateField(ownerCtx, &structurev1.CreateFieldRequest{
		TableId: tableID,
		Name:    "状态",
		Type:    structurev1.FieldType_FIELD_TYPE_SELECT,
		ConfigMap: configMap(t, map[string]any{
			"option_list": []any{
				map[string]any{"id": "opt_t12_todo", "name": "待办", "color": "blue"},
			},
		}),
	})
	if err != nil {
		t.Fatalf("CreateField(select) error = %v", err)
	}
	selectID := selectField.GetField().GetId()

	// 重命名成功 → field_renamed。
	if _, err := harness.client.RenameField(ownerCtx, &structurev1.RenameFieldRequest{
		FieldId: numberID,
		Name:    "数量二",
	}); err != nil {
		t.Fatalf("RenameField error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "field_renamed",
		tableID:    tableID,
		fieldID:    numberID,
		operator:   testOwner,
	})

	// 重名失败（1002）不得新增 field_renamed 事件。
	renamedBefore := countStructureChanged(t, harness, testBaseID, "field_renamed", numberID)
	if _, err := harness.client.RenameField(ownerCtx, &structurev1.RenameFieldRequest{
		FieldId: numberID,
		Name:    "备注",
	}); err == nil {
		t.Fatal("RenameField(duplicate) error = nil, want 1002")
	} else {
		requireErrorCode(t, err, deal.CodeConflict)
	}
	assertStructureChangedCountUnchanged(t, harness, testBaseID, "field_renamed", numberID, renamedBefore)

	// 配置更新成功 → field_updated。
	if _, err := harness.client.UpdateFieldConfig(ownerCtx, &structurev1.UpdateFieldConfigRequest{
		FieldId:   numberID,
		ConfigMap: configMap(t, map[string]any{"decimals": float64(2)}),
	}); err != nil {
		t.Fatalf("UpdateFieldConfig error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "field_updated",
		tableID:    tableID,
		fieldID:    numberID,
		operator:   testOwner,
	})

	// 复制成功 → field_duplicated。
	duplicated, err := harness.client.DuplicateField(ownerCtx, &structurev1.DuplicateFieldRequest{
		FieldId: numberID,
	})
	if err != nil {
		t.Fatalf("DuplicateField error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "field_duplicated",
		tableID:    tableID,
		fieldID:    duplicated.GetField().GetId(),
		operator:   testOwner,
	})

	// 选项维护成功 → field_options_updated。
	if _, err := harness.client.UpdateOptions(ownerCtx, &structurev1.UpdateOptionsRequest{
		FieldId: selectID,
		OptionList: []*structurev1.FieldOption{
			{Id: "opt_t12_todo", Name: "待办", Color: "blue"},
			{Name: "完成", Color: "green"},
		},
	}); err != nil {
		t.Fatalf("UpdateOptions error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "field_options_updated",
		tableID:    tableID,
		fieldID:    selectID,
		operator:   testOwner,
	})

	// 转换预览（confirm=false）不发布；确认后 → field_converted。
	if _, err := harness.client.ConvertField(ownerCtx, &structurev1.ConvertFieldRequest{
		FieldId:    textID,
		TargetType: structurev1.FieldType_FIELD_TYPE_NUMBER,
	}); err != nil {
		t.Fatalf("ConvertField(preview) error = %v", err)
	}
	assertStructureChangedCountUnchanged(t, harness, testBaseID, "field_converted", textID, 0)
	if _, err := harness.client.ConvertField(ownerCtx, &structurev1.ConvertFieldRequest{
		FieldId:    textID,
		TargetType: structurev1.FieldType_FIELD_TYPE_NUMBER,
		Confirm:    true,
	}); err != nil {
		t.Fatalf("ConvertField(confirm) error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "field_converted",
		tableID:    tableID,
		fieldID:    textID,
		operator:   testOwner,
	})

	// 删除预览（confirm=false）不发布；确认后 → field_deleted。
	if _, err := harness.client.DeleteField(ownerCtx, &structurev1.DeleteFieldRequest{
		FieldId: duplicated.GetField().GetId(),
	}); err != nil {
		t.Fatalf("DeleteField(preview) error = %v", err)
	}
	assertStructureChangedCountUnchanged(
		t,
		harness,
		testBaseID,
		"field_deleted",
		duplicated.GetField().GetId(),
		0,
	)
	if _, err := harness.client.DeleteField(ownerCtx, &structurev1.DeleteFieldRequest{
		FieldId: duplicated.GetField().GetId(),
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteField(confirm) error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "field_deleted",
		tableID:    tableID,
		fieldID:    duplicated.GetField().GetId(),
		operator:   testOwner,
	})

	// 事件行随写序严格递增（重放按 outbox 序补发，不重不漏的前提）。
	assertStructureChangedRowIDsIncrease(t, harness, testBaseID)
}

func TestStructureTableWritesPublishStructureChangedEvents(t *testing.T) {
	harness := newStructureHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	cleanupStructureChangedOutbox(t, harness)
	t.Cleanup(func() { cleanupStructureChangedOutbox(t, harness) })

	created, err := harness.client.CreateTable(ownerCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "表事件表",
	})
	if err != nil {
		t.Fatalf("CreateTable error = %v", err)
	}
	tableID := created.GetTable().GetId()
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "table_created",
		tableID:    tableID,
		fieldID:    "",
		operator:   testOwner,
	})

	if _, err := harness.client.RenameTable(ownerCtx, &structurev1.RenameTableRequest{
		TableId: tableID,
		Name:    "表事件表二",
	}); err != nil {
		t.Fatalf("RenameTable error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "table_renamed",
		tableID:    tableID,
		fieldID:    "",
		operator:   testOwner,
	})

	duplicated, err := harness.client.DuplicateTable(ownerCtx, &structurev1.DuplicateTableRequest{
		TableId: tableID,
	})
	if err != nil {
		t.Fatalf("DuplicateTable error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "table_duplicated",
		tableID:    duplicated.GetTable().GetId(),
		fieldID:    "",
		operator:   testOwner,
	})

	// 最后一张表保护路径之外的成功删除 → table_deleted。
	if _, err := harness.client.DeleteTable(ownerCtx, &structurev1.DeleteTableRequest{
		TableId: duplicated.GetTable().GetId(),
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteTable(confirm) error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "table_deleted",
		tableID:    duplicated.GetTable().GetId(),
		fieldID:    "",
		operator:   testOwner,
	})
	assertStructureChangedRowIDsIncrease(t, harness, testBaseID)
}

func TestStructureViewWritesPublishStructureChangedEvents(t *testing.T) {
	harness := newStructureHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	cleanupStructureChangedOutbox(t, harness)
	t.Cleanup(func() { cleanupStructureChangedOutbox(t, harness) })

	table, err := harness.client.CreateTable(ownerCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "视图事件表",
	})
	if err != nil {
		t.Fatalf("CreateTable error = %v", err)
	}
	tableID := table.GetTable().GetId()

	created, err := harness.client.CreateView(ownerCtx, &structurev1.CreateViewRequest{
		TableId: tableID,
		Name:    "看板",
	})
	if err != nil {
		t.Fatalf("CreateView error = %v", err)
	}
	viewID := created.GetView().GetId()
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "view_created",
		tableID:    tableID,
		fieldID:    "",
		operator:   testOwner,
	})

	if _, err := harness.client.RenameView(ownerCtx, &structurev1.RenameViewRequest{
		ViewId: viewID,
		Name:   "看板二",
	}); err != nil {
		t.Fatalf("RenameView error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "view_renamed",
		tableID:    tableID,
		fieldID:    "",
		operator:   testOwner,
	})

	if _, err := harness.client.UpdateViewConfig(ownerCtx, &structurev1.UpdateViewConfigRequest{
		ViewId: viewID,
		ViewConfig: &structurev1.ViewConfig{
			HiddenFieldList: []string{},
		},
	}); err != nil {
		t.Fatalf("UpdateViewConfig error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "view_updated",
		tableID:    tableID,
		fieldID:    "",
		operator:   testOwner,
	})

	duplicated, err := harness.client.DuplicateView(ownerCtx, &structurev1.DuplicateViewRequest{
		ViewId: viewID,
	})
	if err != nil {
		t.Fatalf("DuplicateView error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "view_duplicated",
		tableID:    tableID,
		fieldID:    "",
		operator:   testOwner,
	})

	if _, err := harness.client.DeleteView(ownerCtx, &structurev1.DeleteViewRequest{
		ViewId: duplicated.GetView().GetId(),
	}); err != nil {
		t.Fatalf("DeleteView error = %v", err)
	}
	assertStructureChangedPublished(t, harness, testBaseID, expectedStructureEvent{
		changeType: "view_deleted",
		tableID:    tableID,
		fieldID:    "",
		operator:   testOwner,
	})
	assertStructureChangedRowIDsIncrease(t, harness, testBaseID)
}

type expectedStructureEvent struct {
	changeType string
	tableID    string
	fieldID    string
	operator   string
}

func listStructureChangedOutbox(
	t *testing.T,
	harness *structureHarness,
	baseID string,
) []map[string]any {
	t.Helper()
	rows, err := harness.db.QueryContext(context.Background(), `
		SELECT id, base_id, payload
		FROM excel_bus_outbox
		WHERE type = 'record.changed' AND producer = 'structure' AND base_id = ?
		ORDER BY id
	`, baseID)
	if err != nil {
		t.Fatalf("query structure changed outbox: %v", err)
	}
	defer rows.Close()
	events := make([]map[string]any, 0)
	for rows.Next() {
		var rowID int64
		var eventBaseID string
		var payload []byte
		if err := rows.Scan(&rowID, &eventBaseID, &payload); err != nil {
			t.Fatalf("scan structure changed outbox row: %v", err)
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("decode structure changed payload: %v", err)
		}
		events = append(events, map[string]any{
			"row_id":  rowID,
			"base_id": eventBaseID,
			"payload": decoded,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate structure changed outbox rows: %v", err)
	}
	return events
}

func assertStructureChangedPublished(
	t *testing.T,
	harness *structureHarness,
	baseID string,
	want expectedStructureEvent,
) {
	t.Helper()
	matches := 0
	for _, event := range listStructureChangedOutbox(t, harness, baseID) {
		payload := event["payload"].(map[string]any)
		if payload["change_type"] != want.changeType {
			continue
		}
		matches++
		fieldID, _ := payload["field_id"].(string)
		if payload["table_id"] != want.tableID ||
			fieldID != want.fieldID ||
			payload["operator"] != want.operator {
			t.Fatalf("structure event payload = %v, want change_type=%s table=%s field=%s operator=%s",
				payload, want.changeType, want.tableID, want.fieldID, want.operator)
		}
	}
	if matches != 1 {
		t.Fatalf("structure events for %s = %d, want 1; rows = %+v",
			want.changeType, matches, listStructureChangedOutbox(t, harness, baseID))
	}
}

func countStructureChanged(
	t *testing.T,
	harness *structureHarness,
	baseID, changeType, fieldID string,
) int {
	t.Helper()
	matches := 0
	for _, event := range listStructureChangedOutbox(t, harness, baseID) {
		payload := event["payload"].(map[string]any)
		eventFieldID, _ := payload["field_id"].(string)
		if payload["change_type"] == changeType && eventFieldID == fieldID {
			matches++
		}
	}
	return matches
}

// assertStructureChangedCountUnchanged 锁「失败/预览写不发布」：同一 (change_type,
// field_id) 的事件数与写入前持平。
func assertStructureChangedCountUnchanged(
	t *testing.T,
	harness *structureHarness,
	baseID, changeType, fieldID string,
	before int,
) {
	t.Helper()
	if got := countStructureChanged(t, harness, baseID, changeType, fieldID); got != before {
		t.Fatalf("structure events %s for field %s = %d, want unchanged %d",
			changeType, fieldID, got, before)
	}
}

func assertStructureChangedRowIDsIncrease(
	t *testing.T,
	harness *structureHarness,
	baseID string,
) {
	t.Helper()
	previous := int64(0)
	for _, event := range listStructureChangedOutbox(t, harness, baseID) {
		rowID := event["row_id"].(int64)
		if rowID <= previous {
			t.Fatalf("structure event row ids not increasing: %d after %d", rowID, previous)
		}
		previous = rowID
	}
}

func cleanupStructureChangedOutbox(t *testing.T, harness *structureHarness) {
	t.Helper()
	if _, err := harness.db.ExecContext(context.Background(), `
		DELETE FROM excel_bus_outbox
		WHERE type = 'record.changed' AND producer = 'structure'
	`); err != nil {
		t.Fatalf("cleanup structure changed outbox: %v", err)
	}
}
