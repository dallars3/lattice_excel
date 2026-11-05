package realtime_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

func TestNotificationTargetsUserAcrossConnectionsAndSkipsOthers(t *testing.T) {
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

	aliceTable1 := dialSession(t, server, "session-alice")
	joinRoom(t, aliceTable1, "join-alice-t1", "base_1", "table_1")
	aliceTable2 := dialSession(t, server, "session-alice")
	joinRoom(t, aliceTable2, "join-alice-t2", "base_1", "table_2")
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_1")

	publishNotifyCreated(t, bus, map[string]any{
		"notification_id": "ntf_1",
		"user_id":         "usr_alice",
		"type":            "comment.mentioned",
		"base_id":         "base_1",
	}, 1710000000000)

	for name, connection := range map[string]*websocket.Conn{
		"alice_table_1": aliceTable1,
		"alice_table_2": aliceTable2,
	} {
		notification := readNotification(t, connection, time.Second)
		if notification.GetNotificationId() != "ntf_1" ||
			notification.GetType() != "comment.mentioned" ||
			notification.GetBaseId() != "base_1" ||
			notification.GetOccurredAtMs() != 1710000000000 {
			t.Fatalf("%s notification = %v, want ntf_1/comment.mentioned/base_1/1710000000000",
				name, notification)
		}
	}

	assertNoNotification(t, bob)
}

func TestNotificationIsNotReplayed(t *testing.T) {
	store := &replayAllStore{frames: []*webapiv1.Frame{
		cellChangedFrame(21, "rec_1", "fld_1", "usr_bob"),
		{
			Payload: &webapiv1.Frame_Notification{Notification: &webapiv1.Notification{
				NotificationId: "ntf_1",
				Type:           "comment.mentioned",
				BaseId:         "base_1",
				OccurredAtMs:   1710000000000,
			}},
		},
	}}
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:      store,
		Bus:         eventbus.NewMemoryBus(),
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")

	sendFrame(t, alice, &webapiv1.Frame{
		MsgId: "replay-1",
		Payload: &webapiv1.Frame_ReplayRequest{ReplayRequest: &webapiv1.ReplayRequest{
			LastSeq: 20,
		}},
	})

	replayed := int64(0)
	for {
		frame := readFrameWithin(t, alice, time.Second)
		if frame.GetNotification() != nil {
			t.Fatalf("replay delivered notification: %v", frame.GetNotification())
		}
		if changed := frame.GetCellChanged(); changed != nil {
			replayed = changed.GetSeq()
		}
		if frame.GetAck().GetMsgId() == "replay-1" {
			break
		}
	}
	if replayed != 21 {
		t.Fatalf("replayed cell_changed seq = %d, want 21", replayed)
	}
}

func TestNotificationFrameWireShapeHasNoSequence(t *testing.T) {
	frame := &webapiv1.Frame{Payload: &webapiv1.Frame_Notification{
		Notification: &webapiv1.Notification{
			NotificationId: "ntf_1",
			Type:           "comment.mentioned",
			BaseId:         "base_1",
			OccurredAtMs:   1710000000000,
		},
	}}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal notification frame: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal notification frame json: %v", err)
	}
	if _, exists := decoded["seq"]; exists {
		t.Fatalf("notification frame carries seq field: %s", raw)
	}
	notification, ok := decoded["notification"].(map[string]any)
	if !ok {
		t.Fatalf("notification payload missing: %s", raw)
	}
	if notification["notification_id"] != "ntf_1" ||
		notification["type"] != "comment.mentioned" ||
		notification["base_id"] != "base_1" ||
		notification["occurred_at_ms"] != "1710000000000" {
		t.Fatalf("notification payload = %v, want snake_case fields without seq", notification)
	}
}

type replayAllStore struct {
	frames []*webapiv1.Frame
}

func (store *replayAllStore) Since(
	_ context.Context,
	_ string,
	_ string,
	_ int64,
	_ int,
) ([]*webapiv1.Frame, error) {
	return store.frames, nil
}

func publishNotifyCreated(
	t *testing.T,
	bus *eventbus.MemoryBus,
	payload map[string]any,
	occurredAt int64,
) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal notify.created payload: %v", err)
	}
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "evt_ntf",
			Type:       "notify.created",
			Version:    1,
			BaseID:     "base_1",
			OccurredAt: occurredAt,
			Producer:   "notify",
		},
		Payload: raw,
	}
	if err := bus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish notify.created: %v", err)
	}
}

func readNotification(
	t *testing.T,
	connection *websocket.Conn,
	timeout time.Duration,
) *webapiv1.Notification {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		frame := readFrameWithin(t, connection, time.Until(deadline))
		if notification := frame.GetNotification(); notification != nil {
			return notification
		}
	}
}

func assertNoNotification(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	for {
		_, payload, err := connection.ReadMessage()
		if err != nil {
			return
		}
		var frame webapiv1.Frame
		if err := protojson.Unmarshal(payload, &frame); err != nil {
			t.Fatalf("unmarshal unrelated frame: %v", err)
		}
		if frame.GetNotification() != nil {
			t.Fatalf("unrelated user received notification: %v", frame.GetNotification())
		}
	}
}