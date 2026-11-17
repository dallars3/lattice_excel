package realtime_test

import (
	"context"
	"encoding/base64"
	"io"
	"testing"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

// scriptedEventStream replays a canned records.GetEventsSince stream.
type scriptedEventStream struct {
	responses []*recordsv1.GetEventsSinceResponse
	err       error
	index     int
}

func (stream *scriptedEventStream) Recv() (*recordsv1.GetEventsSinceResponse, error) {
	if stream.err != nil {
		return nil, stream.err
	}
	if stream.index >= len(stream.responses) {
		return nil, io.EOF
	}
	response := stream.responses[stream.index]
	stream.index++
	return response, nil
}

func (stream *scriptedEventStream) Header() (metadata.MD, error) { return nil, nil }
func (stream *scriptedEventStream) Trailer() metadata.MD         { return nil }
func (stream *scriptedEventStream) CloseSend() error             { return nil }
func (stream *scriptedEventStream) Context() context.Context     { return context.Background() }
func (stream *scriptedEventStream) SendMsg(any) error            { return nil }
func (stream *scriptedEventStream) RecvMsg(any) error            { return nil }

// replayEventsFake captures the outgoing request/metadata and serves a scripted
// stream, standing in for core records.GetEventsSince.
type replayEventsFake struct {
	stream   *scriptedEventStream
	err      error
	requests []*recordsv1.GetEventsSinceRequest
	metadata []metadata.MD
}

func (fake *replayEventsFake) GetEventsSince(
	ctx context.Context,
	request *recordsv1.GetEventsSinceRequest,
) (recordsv1.RecordsService_GetEventsSinceClient, error) {
	fake.requests = append(fake.requests, request)
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok {
		fake.metadata = append(fake.metadata, outgoing)
	}
	if fake.err != nil {
		return nil, fake.err
	}
	return fake.stream, nil
}

func syncEvent(eventType string, seq int64, payload map[string]any) *recordsv1.GetEventsSinceResponse {
	structPayload, err := structpb.NewStruct(payload)
	if err != nil {
		panic(err)
	}
	return &recordsv1.GetEventsSinceResponse{
		Event: &recordsv1.SyncEvent{
			Header: &commonv1.EventHeader{
				EventId:  "evt_" + eventType,
				Type:     eventType,
				Version:  1,
				BaseId:   "base_1",
				Seq:      seq,
				Producer: "structure",
			},
			PayloadMap: structPayload,
		},
	}
}

func TestEventStoreSinceConvertsDurableEventsIntoReplayFrames(t *testing.T) {
	fake := &replayEventsFake{stream: &scriptedEventStream{responses: []*recordsv1.GetEventsSinceResponse{
		syncEvent("record.changed", 31, map[string]any{
			"table_id":    "table_1",
			"field_id":    "fld_new",
			"change_type": "field_created",
			"operator":    "usr_manager",
		}),
		syncEvent("record.changed", 32, map[string]any{
			"table_id":    "table_1",
			"record_id":   "rec_1",
			"field_id":    "fld_1",
			"value":       42,
			"version":     7,
			"operator":    "usr_bob",
			"change_type": "update",
		}),
		syncEvent("record.deleted", 33, map[string]any{
			"table_id":       "table_1",
			"record_id_list": []any{"rec_2", "rec_3"},
		}),
		// 通知/审计类事件不是增量帧，永不进入补发（W-17 不进快照与重放）。
		syncEvent("notify.created", 34, map[string]any{
			"notification_id": "ntf_1",
			"user_id":         "usr_alice",
		}),
		syncEvent("table.deleted", 35, map[string]any{
			"table_id": "table_1",
		}),
	}}}
	store := realtime.NewRPCEventStore(fake)
	principal := &commonv1.Principal{UserId: "usr_alice", Internal: true}

	frames, err := store.Since(context.Background(), principal, "base_1", "table_1", 30, 15)
	if err != nil {
		t.Fatalf("EventStore.Since() error = %v", err)
	}

	structureFrames := 0
	deletedRecords := make([]string, 0, 2)
	for _, frame := range frames {
		switch payload := frame.GetPayload().(type) {
		case *webapiv1.Frame_StructureChanged:
			structureFrames++
			changed := payload.StructureChanged
			if changed.GetSeq() != 31 ||
				changed.GetChangeType() != "field_created" ||
				changed.GetTableId() != "table_1" ||
				changed.GetFieldId() != "fld_new" {
				t.Fatalf("structure_changed frame = %v, want seq 31 field_created table_1/fld_new", changed)
			}
		case *webapiv1.Frame_CellChanged:
			changed := payload.CellChanged
			if changed.GetSeq() != 32 ||
				changed.GetRecordId() != "rec_1" ||
				changed.GetFieldId() != "fld_1" ||
				changed.GetValue().GetIntValue() != 42 ||
				changed.GetVersion() != 7 ||
				changed.GetOperator() != "usr_bob" {
				t.Fatalf("cell_changed frame = %v, want live broadcast shape at seq 32", changed)
			}
		case *webapiv1.Frame_RecordDeleted:
			deletedRecords = append(deletedRecords, payload.RecordDeleted.GetRecordId())
			if payload.RecordDeleted.GetSeq() != 33 {
				t.Fatalf("record_deleted seq = %d, want 33", payload.RecordDeleted.GetSeq())
			}
		case *webapiv1.Frame_Notification:
			t.Fatalf("notification entered replay: %v", payload.Notification)
		default:
			t.Fatalf("unexpected replay frame payload %T", frame.GetPayload())
		}
	}
	if structureFrames != 1 {
		t.Fatalf("structure_changed frame count = %d, want 1", structureFrames)
	}
	if len(deletedRecords) != 2 || deletedRecords[0] != "rec_2" || deletedRecords[1] != "rec_3" {
		t.Fatalf("record_deleted frames = %v, want one per record at seq 33", deletedRecords)
	}

	if len(fake.requests) != 1 {
		t.Fatalf("GetEventsSince call count = %d, want 1", len(fake.requests))
	}
	request := fake.requests[0]
	if request.GetBaseId() != "base_1" || request.GetTableId() != "table_1" ||
		request.GetLastSeq() != 30 || request.GetLimit() != 15 {
		t.Fatalf("GetEventsSince request = %v, want base_1/table_1/30/15", request)
	}
	if len(fake.metadata) != 1 {
		t.Fatalf("outgoing metadata count = %d, want 1", len(fake.metadata))
	}
	encoded := fake.metadata[0].Get("x-principal")
	if len(encoded) != 1 {
		t.Fatalf("x-principal metadata = %v, want one value", encoded)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded[0])
	if err != nil {
		t.Fatalf("decode x-principal: %v", err)
	}
	var forwarded commonv1.Principal
	if err := proto.Unmarshal(decoded, &forwarded); err != nil || forwarded.GetUserId() != "usr_alice" {
		t.Fatalf("forwarded principal = %v err = %v, want usr_alice", &forwarded, err)
	}
	scope := fake.metadata[0].Get("x-row-scope")
	if len(scope) != 1 || (scope[0] != "ROW_SCOPE_ALL" && scope[0] != "ROW_SCOPE_OWN") {
		t.Fatalf("x-row-scope metadata = %v, want a normalized scope", scope)
	}
}

func TestEventStoreSincePropagatesStreamFailure(t *testing.T) {
	fake := &replayEventsFake{
		err: context.DeadlineExceeded,
	}
	store := realtime.NewRPCEventStore(fake)
	if _, err := store.Since(
		context.Background(),
		&commonv1.Principal{UserId: "usr_alice"},
		"base_1",
		"table_1",
		0,
		15,
	); err == nil {
		t.Fatalf("EventStore.Since() error = nil, want upstream failure propagated")
	}
}
