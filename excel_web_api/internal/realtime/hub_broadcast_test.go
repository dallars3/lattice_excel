package realtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

func TestRecordChangedBroadcastsToOtherMembersWithinWindowAndBatches(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         bus,
		Now:         time.Now,
		FlushWindow: 100 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_1")

	publishedAt := time.Now()
	publishRecordChanged(t, bus, 103, "usr_bob", 42)
	publishRecordChanged(t, bus, 104, "usr_bob", 43)

	first, firstAt := readCellChangedFrame(t, alice, time.Second)
	second, _ := readCellChangedFrame(t, alice, time.Second)
	if delay := firstAt.Sub(publishedAt); delay < 60*time.Millisecond {
		t.Fatalf("first broadcast arrived after %v, want the 100ms coalescing window to apply", delay)
	}
	for index, frame := range []*webapiv1.Frame{first, second} {
		changed := frame.GetCellChanged()
		if changed.GetSeq() != int64(103+index) ||
			changed.GetRecordId() != "rec_1" ||
			changed.GetFieldId() != "fld_1" ||
			changed.GetValue().GetIntValue() != int64(42+index) ||
			changed.GetVersion() != int64(7+index) ||
			changed.GetOperator() != "usr_bob" {
			t.Fatalf("cell_changed[%d] = %v, want seq/value/version/operator from event", index, changed)
		}
	}

	sendFrame(t, bob, &webapiv1.Frame{
		MsgId:   "bob-heartbeat",
		Payload: &webapiv1.Frame_Heartbeat{Heartbeat: &webapiv1.HeartbeatRequest{}},
	})
	for {
		frame := readFrameWithin(t, bob, time.Second)
		if frame.GetCellChanged() != nil {
			t.Fatalf("source operator received own cell_changed: %v", frame.GetCellChanged())
		}
		if frame.GetAck().GetMsgId() == "bob-heartbeat" {
			break
		}
	}
}

func TestRecordDeletedBroadcastsOneFramePerRecord(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         bus,
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")

	payload, err := json.Marshal(map[string]any{
		"table_id":       "table_1",
		"record_id_list": []string{"rec_1", "rec_2"},
	})
	if err != nil {
		t.Fatalf("marshal record.deleted: %v", err)
	}
	if err := bus.Publish(context.Background(), eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "evt_deleted",
			Type:       "record.deleted",
			Version:    1,
			BaseID:     "base_1",
			Seq:        110,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "records",
		},
		Payload: payload,
	}); err != nil {
		t.Fatalf("publish record.deleted: %v", err)
	}

	deleted := make(map[string]int64)
	for len(deleted) < 2 {
		frame := readFrameWithin(t, alice, time.Second)
		if item := frame.GetRecordDeleted(); item != nil {
			deleted[item.GetRecordId()] = item.GetSeq()
		}
	}
	if deleted["rec_1"] != 110 || deleted["rec_2"] != 110 {
		t.Fatalf("record_deleted map = %v, want rec_1/rec_2 at seq 110", deleted)
	}
}

func TestRecordChangedStructureEventBroadcastsStructureChanged(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         bus,
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")
	payload, err := json.Marshal(map[string]any{
		"table_id":    "table_1",
		"field_id":    "fld_1",
		"version":     3,
		"operator":    "usr_manager",
		"change_type": "field_renamed",
	})
	if err != nil {
		t.Fatalf("marshal structure event: %v", err)
	}
	if err := bus.Publish(context.Background(), eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "evt_structure",
			Type:       "record.changed",
			Version:    1,
			BaseID:     "base_1",
			Seq:        120,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "structure",
		},
		Payload: payload,
	}); err != nil {
		t.Fatalf("publish structure event: %v", err)
	}

	for {
		frame := readFrameWithin(t, alice, time.Second)
		if changed := frame.GetStructureChanged(); changed != nil {
			if changed.GetSeq() != 120 ||
				changed.GetChangeType() != "field_renamed" ||
				changed.GetTableId() != "table_1" ||
				changed.GetFieldId() != "fld_1" {
				t.Fatalf("structure_changed = %v, want event payload", changed)
			}
			return
		}
		if frame.GetCellChanged() != nil {
			t.Fatalf("structure event was broadcast as cell_changed: %v", frame.GetCellChanged())
		}
	}
}

func publishRecordChanged(
	t *testing.T,
	bus *eventbus.MemoryBus,
	seq int64,
	operator string,
	value int64,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"table_id":    "table_1",
		"record_id":   "rec_1",
		"field_id":    "fld_1",
		"value":       value,
		"version":     7 + seq - 103,
		"operator":    operator,
		"change_type": "cell_update",
	})
	if err != nil {
		t.Fatalf("marshal record.changed payload: %v", err)
	}
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    fmt.Sprintf("evt_%d", seq),
			Type:       "record.changed",
			Version:    1,
			BaseID:     "base_1",
			Seq:        seq,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "records",
		},
		Payload: payload,
	}
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish record.changed: %v", err)
	}
}

func readCellChangedFrame(
	t *testing.T,
	connection *websocket.Conn,
	timeout time.Duration,
) (*webapiv1.Frame, time.Time) {
	t.Helper()
	for {
		frame := readFrameWithin(t, connection, timeout)
		if frame.GetCellChanged() != nil {
			return frame, time.Now()
		}
	}
}

func newRealtimeServer(t *testing.T, hub *realtime.Hub) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(hub.Handler())
	t.Cleanup(server.Close)
	return server
}

func joinRoom(
	t *testing.T,
	connection *websocket.Conn,
	msgID, baseID, tableID string,
) {
	t.Helper()
	sendFrame(t, connection, &webapiv1.Frame{
		MsgId: msgID,
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  baseID,
			TableId: tableID,
		}},
	})
	for {
		frame := readFrameWithin(t, connection, time.Second)
		if frame.GetSnapshot() != nil {
			return
		}
	}
}
