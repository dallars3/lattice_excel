package realtime_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

// TestFramesOverWebSocketCarryExplicitZeroSequence 锁线上帧形状：快照、单元格变更、
// 结构变更、记录删除都必须显式携带 seq（含 0），供客户端判断事件顺序。
func TestFramesOverWebSocketCarryExplicitZeroSequence(t *testing.T) {
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
	sendFrame(t, alice, &webapiv1.Frame{
		MsgId: "join-alice",
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  "base_1",
			TableId: "table_1",
		}},
	})
	var snapshot map[string]json.RawMessage
	for snapshot == nil {
		document := readRawFrame(t, alice, 2*time.Second)
		if raw, ok := document["snapshot"]; ok {
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				t.Fatalf("snapshot is not an object: %s", raw)
			}
		}
	}
	assertFrameSequence(t, snapshot)
	if _, ok := snapshot["member_list"]; !ok {
		t.Fatalf("snapshot omitted member_list: %v", snapshot)
	}

	for _, publish := range []func(){
		func() {
			publishEvent(t, bus, eventbus.Event{
				Header: eventbus.EventHeader{
					EventID: "evt_zero_cell", Type: "record.changed", Version: 1,
					BaseID: "base_1", Seq: 0, OccurredAt: time.Now().UnixMilli(),
					Producer: "records",
				},
				Payload: json.RawMessage(`{"table_id":"table_1","record_id":"rec_1","field_id":"fld_1","value":1,"version":1,"change_type":"cell_update"}`),
			})
		},
		func() {
			publishEvent(t, bus, eventbus.Event{
				Header: eventbus.EventHeader{
					EventID: "evt_zero_structure", Type: "record.changed", Version: 1,
					BaseID: "base_1", Seq: 0, OccurredAt: time.Now().UnixMilli(),
					Producer: "structure",
				},
				Payload: json.RawMessage(`{"table_id":"table_1","change_type":"field_renamed"}`),
			})
		},
		func() {
			publishEvent(t, bus, eventbus.Event{
				Header: eventbus.EventHeader{
					EventID: "evt_zero_deleted", Type: "record.deleted", Version: 1,
					BaseID: "base_1", Seq: 0, OccurredAt: time.Now().UnixMilli(),
					Producer: "records",
				},
				Payload: json.RawMessage(`{"table_id":"table_1","record_id_list":["rec_1"]}`),
			})
		},
	} {
		publish()
	}

	wanted := []string{"cell_changed", "structure_changed", "record_deleted"}
	received := make(map[string]map[string]json.RawMessage, len(wanted))
	deadline := time.Now().Add(3 * time.Second)
	for len(received) < len(wanted) {
		document := readRawFrame(t, alice, time.Until(deadline))
		for _, name := range wanted {
			if _, seen := received[name]; seen {
				continue
			}
			raw, ok := document[name]
			if !ok {
				continue
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatalf("%s payload is not an object: %s", name, raw)
			}
			received[name] = object
		}
	}
	for _, name := range wanted {
		assertFrameSequence(t, received[name])
	}
}

func readRawFrame(
	t *testing.T,
	connection *websocket.Conn,
	timeout time.Duration,
) map[string]json.RawMessage {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("frame is not a JSON object: %s", payload)
	}
	return document
}

func assertFrameSequence(t *testing.T, payload map[string]json.RawMessage) {
	t.Helper()
	seq, ok := payload["seq"]
	if !ok {
		t.Fatalf("frame omitted zero seq: %v", payload)
	}
	if string(seq) != `"0"` && string(seq) != `0` {
		t.Fatalf("seq = %s, want 0", seq)
	}
}

func publishEvent(t *testing.T, bus *eventbus.MemoryBus, event eventbus.Event) {
	t.Helper()
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish %s: %v", event.Header.Type, err)
	}
}
