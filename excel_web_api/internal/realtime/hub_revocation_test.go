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

func TestLinkClosedRevokesEveryRoomInBaseImmediately(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: false},
			"session-other": {UserId: "usr_other", Internal: true},
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
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_2")
	other := dialSession(t, server, "session-other")
	joinRoom(t, other, "join-other", "base_2", "table_1")

	payload, err := json.Marshal(map[string]any{
		"base_id": "base_1",
		"link_id": "link_1",
		"reason":  "force_closed",
	})
	if err != nil {
		t.Fatalf("marshal link.closed: %v", err)
	}
	publishedAt := time.Now()
	if err := bus.Publish(context.Background(), eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "evt_link_closed",
			Type:       "link.closed",
			Version:    1,
			BaseID:     "base_1",
			Seq:        200,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "share",
		},
		Payload: payload,
	}); err != nil {
		t.Fatalf("publish link.closed: %v", err)
	}

	for name, connection := range map[string]*websocket.Conn{
		"alice": alice,
		"bob":   bob,
	} {
		readSessionRevoked(t, connection, name)
	}
	if elapsed := time.Since(publishedAt); elapsed >= time.Second {
		t.Fatalf("session_revoked delay = %v, want immediate push below 1s", elapsed)
	}

	if err := other.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	for {
		_, payload, err := other.ReadMessage()
		if err != nil {
			break
		}
		var frame webapiv1.Frame
		if err := protojson.Unmarshal(payload, &frame); err != nil {
			t.Fatalf("unmarshal unrelated frame: %v", err)
		}
		if frame.GetSessionRevoked() != nil {
			t.Fatalf("unrelated base received session_revoked: %v", frame.GetSessionRevoked())
		}
	}
}

func readSessionRevoked(t *testing.T, connection *websocket.Conn, name string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		frame := readFrameWithin(t, connection, time.Until(deadline))
		if frame.GetSessionRevoked().GetReason() == "link_closed" {
			return
		}
		if frame.GetSessionRevoked() != nil {
			t.Fatalf("%s session_revoked = %v, want link_closed", name, frame)
		}
	}
}
