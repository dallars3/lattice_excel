package structure_test

import (
	"context"
	"encoding/json"
	"testing"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// 缺陷 #23：表删除提交后必须在同一事务内把 table.deleted 落 outbox（audit 消费
// 落 TABLE_DELETED）；confirm=false 与最后一张表保护（4003）都不得产生事件。
// 断言持久 outbox 行而非内存转发：其它测试包的 forwarder 会并发认领共享 outbox。
// 事件经 broker→audit 消费的完整链路由 internal/audit 的消费测试覆盖。
func TestDeleteTablePublishesTableDeletedEvent(t *testing.T) {
	harness := newStructureHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	cleanupTableDeletedOutbox(t, harness)
	t.Cleanup(func() { cleanupTableDeletedOutbox(t, harness) })

	keeper, err := harness.client.CreateTable(ownerCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "保留表",
	})
	if err != nil {
		t.Fatalf("CreateTable(keeper) error = %v", err)
	}
	dropped, err := harness.client.CreateTable(ownerCtx, &structurev1.CreateTableRequest{
		BaseId: testBaseID,
		Name:   "待删除表",
	})
	if err != nil {
		t.Fatalf("CreateTable(dropped) error = %v", err)
	}

	// confirm=false：不删除，不发布。
	if _, err := harness.client.DeleteTable(ownerCtx, &structurev1.DeleteTableRequest{
		TableId: dropped.GetTable().GetId(),
	}); err != nil {
		t.Fatalf("DeleteTable(confirm=false) error = %v", err)
	}
	assertTableDeletedOutboxCount(t, harness, 0)

	// confirm=true：删除成功并发布 table.deleted。
	if _, err := harness.client.DeleteTable(ownerCtx, &structurev1.DeleteTableRequest{
		TableId: dropped.GetTable().GetId(),
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteTable(confirm=true) error = %v", err)
	}
	events := listTableDeletedOutbox(t, harness)
	if len(events) != 1 {
		t.Fatalf("table.deleted outbox rows = %d, want 1; rows = %+v", len(events), events)
	}
	event := events[0]
	var payload map[string]any
	if err := json.Unmarshal([]byte(event["payload"]), &payload); err != nil {
		t.Fatalf("decode table.deleted payload: %v", err)
	}
	if event["base_id"] != testBaseID ||
		payload["enterprise_id"] != testEnterprise ||
		payload["base_id"] != testBaseID ||
		payload["table_id"] != dropped.GetTable().GetId() ||
		payload["table_name"] != "待删除表" ||
		payload["operator"] != testOwner {
		t.Fatalf("table.deleted outbox row = %+v payload = %v", event, payload)
	}

	// 最后一张表保护（H-33/4003）：不删除，不发布。
	if _, err := harness.client.DeleteTable(ownerCtx, &structurev1.DeleteTableRequest{
		TableId: keeper.GetTable().GetId(),
		Confirm: true,
	}); err == nil {
		t.Fatal("DeleteTable(last table) error = nil, want 4003")
	} else {
		requireErrorCode(t, err, deal.CodeLastTableProtected)
	}
	assertTableDeletedOutboxCount(t, harness, 1)
}

func listTableDeletedOutbox(
	t *testing.T,
	harness *structureHarness,
) []map[string]string {
	t.Helper()
	rows, err := harness.db.QueryContext(context.Background(), `
		SELECT event_id, type, base_id, payload
		FROM excel_bus_outbox
		WHERE type = 'table.deleted'
		ORDER BY id
	`)
	if err != nil {
		t.Fatalf("query table.deleted outbox: %v", err)
	}
	defer rows.Close()
	events := make([]map[string]string, 0)
	for rows.Next() {
		var eventID, eventType, baseID string
		var payload []byte
		if err := rows.Scan(&eventID, &eventType, &baseID, &payload); err != nil {
			t.Fatalf("scan table.deleted outbox row: %v", err)
		}
		events = append(events, map[string]string{
			"event_id": eventID,
			"type":     eventType,
			"base_id":  baseID,
			"payload":  string(payload),
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table.deleted outbox rows: %v", err)
	}
	return events
}

func assertTableDeletedOutboxCount(
	t *testing.T,
	harness *structureHarness,
	want int,
) {
	t.Helper()
	if events := listTableDeletedOutbox(t, harness); len(events) != want {
		t.Fatalf("table.deleted outbox rows = %d, want %d; rows = %+v", len(events), want, events)
	}
}

func cleanupTableDeletedOutbox(t *testing.T, harness *structureHarness) {
	t.Helper()
	if _, err := harness.db.ExecContext(
		context.Background(),
		"DELETE FROM excel_bus_outbox WHERE type = 'table.deleted'",
	); err != nil {
		t.Fatalf("cleanup table.deleted outbox: %v", err)
	}
}
