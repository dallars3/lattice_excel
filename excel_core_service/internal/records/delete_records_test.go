package records_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

func TestDeleteRecordsSupportsUndoWindowAndPublishesDeletedEvent(t *testing.T) {
	access := writeStructureAccessor(&structurev1.Field{
		Id:        "fld_title",
		TableId:   recordsTestTableID,
		Name:      "Title",
		Type:      structurev1.FieldType_FIELD_TYPE_TEXT,
		IsPrimary: true,
	})
	harness := newWriteHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_OWN"},
		nil,
	)
	seedRecord(
		t,
		harness.db,
		"rec_delete_undo",
		recordsTestTableID,
		recordsTestUserID,
		4,
		map[string]*commonv1.Value{"fld_title": stringCell("keep me")},
	)
	seedRecord(
		t,
		harness.db,
		"rec_delete_expired",
		recordsTestTableID,
		recordsTestUserID,
		2,
		map[string]*commonv1.Value{"fld_title": stringCell("too late")},
	)
	subscription, err := harness.memoryBus.Subscribe(
		context.Background(),
		eventbus.SubscribeOptions{
			Topic: "record.deleted",
			Mode:  eventbus.DeliveryModePubSub,
		},
	)
	if err != nil {
		t.Fatalf("subscribe record.deleted: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })

	request := &recordsv1.DeleteRecordsRequest{
		RecordIdList:    []string{"rec_delete_undo", "rec_delete_expired"},
		ClientRequestId: "req_delete_001",
	}
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN")
	deleted, err := harness.client.DeleteRecords(auth, request)
	if err != nil {
		t.Fatalf("DeleteRecords() error = %v", err)
	}
	if deleted.GetDeletedCount() != 2 {
		t.Fatalf("deleted_count = %d, want 2", deleted.GetDeletedCount())
	}
	for _, recordID := range request.GetRecordIdList() {
		if _, err := base.NewRecordsRepository(harness.db).RecordByID(
			context.Background(),
			recordID,
		); err != base.ErrRecordNotFound {
			t.Fatalf("RecordByID(%s) error = %v, want ErrRecordNotFound", recordID, err)
		}
	}
	assertDatabaseCount(t, harness.db, "excel_records_histories", 2)

	forwarded, err := harness.forwarder.ForwardOnce(context.Background())
	if err != nil || forwarded != 1 {
		t.Fatalf("ForwardOnce() = %d, %v, want one event", forwarded, err)
	}
	receiveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	delivery, err := subscription.Receive(receiveCtx)
	if err != nil {
		t.Fatalf("receive record.deleted: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode record.deleted payload: %v", err)
	}
	ids, _ := payload["record_id_list"].([]any)
	if delivery.Event.Header.Type != "record.deleted" ||
		payload["enterprise_id"] != "01J00000000000000000001006" ||
		payload["table_id"] != recordsTestTableID ||
		len(ids) != 2 {
		t.Fatalf("record.deleted event = header:%+v payload:%v", delivery.Event.Header, payload)
	}

	replayed, err := harness.client.DeleteRecords(auth, request)
	if err != nil {
		t.Fatalf("DeleteRecords(replay) error = %v", err)
	}
	if replayed.GetDeletedCount() != 2 {
		t.Fatalf("replayed deleted_count = %d, want 2", replayed.GetDeletedCount())
	}
	assertDatabaseCount(t, harness.db, "excel_records_histories", 2)

	undone, err := harness.client.UndoDelete(auth, &recordsv1.UndoDeleteRequest{
		RecordId: "rec_delete_undo",
	})
	if err != nil {
		t.Fatalf("UndoDelete() error = %v", err)
	}
	if undone.GetRecord().GetId() != "rec_delete_undo" ||
		undone.GetRecord().GetVersion() != 6 {
		t.Fatalf("UndoDelete response = %v, want active record version 6", undone.GetRecord())
	}
	restored, err := base.NewRecordsRepository(harness.db).RecordByID(
		context.Background(),
		"rec_delete_undo",
	)
	if err != nil {
		t.Fatalf("RecordByID(restored) error = %v", err)
	}
	if restored.Cells["fld_title"].GetStringValue() != "keep me" {
		t.Fatalf("restored cells = %v", restored.Cells)
	}

	expiredAt := harness.now.Add(-7*time.Second - time.Millisecond).UnixMilli()
	if _, err := harness.db.ExecContext(
		context.Background(),
		"UPDATE excel_records SET soft_deleted_at = ? WHERE id = ?",
		expiredAt,
		"rec_delete_expired",
	); err != nil {
		t.Fatalf("age soft-deleted row: %v", err)
	}
	_, err = harness.client.UndoDelete(auth, &recordsv1.UndoDeleteRequest{
		RecordId: "rec_delete_expired",
	})
	requireRPCErrorCode(t, err, deal.CodeDeletedResource)
}
