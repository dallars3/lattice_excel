package records_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
)

func TestCreateRecordIsIdempotentForTwentyFourHoursAndForcesCreator(t *testing.T) {
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
	request := &recordsv1.CreateRecordRequest{
		TableId: recordsTestTableID,
		CellsMap: map[string]*commonv1.Value{
			"fld_title": stringCell("one"),
		},
		ClientRequestId: "req_create_001",
	}
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_OWN")
	first, err := harness.client.CreateRecord(auth, request)
	if err != nil {
		t.Fatalf("CreateRecord(first) error = %v", err)
	}
	second, err := harness.client.CreateRecord(auth, request)
	if err != nil {
		t.Fatalf("CreateRecord(replay) error = %v", err)
	}
	if !proto.Equal(first, second) {
		t.Fatalf("CreateRecord replay = %v, want %v", second, first)
	}
	if first.GetRecord().GetId() == "" ||
		first.GetRecord().GetVersion() != 1 ||
		first.GetRecord().GetCreatedBy() != recordsTestUserID {
		t.Fatalf("created record = %v", first.GetRecord())
	}
	assertDatabaseCount(t, harness.db, "excel_records", 1)
	assertDatabaseCount(t, harness.db, "excel_records_histories", 1)
	assertDatabaseCount(t, harness.db, "excel_bus_outbox", 1)

	if _, err := harness.db.ExecContext(
		context.Background(),
		"UPDATE excel_records_oplog SET created_at = ? WHERE client_request_id = ?",
		harness.now.Add(-24*time.Hour).UnixMilli(),
		request.GetClientRequestId(),
	); err != nil {
		t.Fatalf("expire create oplog: %v", err)
	}
	third, err := harness.client.CreateRecord(auth, request)
	if err != nil {
		t.Fatalf("CreateRecord(expired replay) error = %v", err)
	}
	if third.GetRecord().GetId() == first.GetRecord().GetId() {
		t.Fatalf("expired idempotency key reused record %s", first.GetRecord().GetId())
	}
	assertDatabaseCount(t, harness.db, "excel_records", 2)
}

func TestCreateRecordConcurrentClientRequestReturnsOneRecord(t *testing.T) {
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
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		nil,
	)
	request := &recordsv1.CreateRecordRequest{
		TableId: recordsTestTableID,
		CellsMap: map[string]*commonv1.Value{
			"fld_title": stringCell("concurrent"),
		},
		ClientRequestId: "req_create_concurrent",
	}
	auth := recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL")

	type outcome struct {
		response *recordsv1.CreateRecordResponse
		err      error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for attempt := 0; attempt < 2; attempt++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response, err := harness.client.CreateRecord(auth, request)
			outcomes <- outcome{response: response, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(outcomes)

	var recordID string
	for result := range outcomes {
		if result.err != nil {
			t.Fatalf("concurrent CreateRecord() error = %v", result.err)
		}
		gotID := result.response.GetRecord().GetId()
		if recordID == "" {
			recordID = gotID
		} else if gotID != recordID {
			t.Fatalf("concurrent record ids = %s and %s, want one id", recordID, gotID)
		}
	}
	assertDatabaseCount(t, harness.db, "excel_records", 1)
	assertDatabaseCount(t, harness.db, "excel_records_histories", 1)
	assertDatabaseCount(t, harness.db, "excel_bus_outbox", 1)
}
