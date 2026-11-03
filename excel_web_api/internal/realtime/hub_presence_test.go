package realtime_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

func TestPresenceCursorSelectionEditingHeartbeatAndStableColors(t *testing.T) {
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: false},
		}},
		Events:           &eventStoreFake{},
		Bus:              eventbus.NewMemoryBus(),
		Now:              time.Now,
		FlushWindow:      5 * time.Millisecond,
		HeartbeatTimeout: 2 * time.Second,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_1")

	online := readPresence(t, alice, "usr_bob", true, time.Second)
	if online.GetInternal() || online.GetColor() == "" {
		t.Fatalf("external presence = %v, want external label and color", online)
	}
	firstColor := online.GetColor()

	_ = bob.Close()
	offline := readPresence(t, alice, "usr_bob", false, time.Second)
	if offline.GetColor() != firstColor {
		t.Fatalf("offline color = %q, want stable color %q", offline.GetColor(), firstColor)
	}

	bob = dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob-again", "base_1", "table_1")
	onlineAgain := readPresence(t, alice, "usr_bob", true, time.Second)
	if onlineAgain.GetColor() != firstColor {
		t.Fatalf("reconnected color = %q, want stable color %q",
			onlineAgain.GetColor(), firstColor)
	}

	sendFrame(t, bob, &webapiv1.Frame{
		MsgId:   "heartbeat-bob",
		Payload: &webapiv1.Frame_Heartbeat{Heartbeat: &webapiv1.HeartbeatRequest{}},
	})
	readAck(t, bob, "heartbeat-bob")

	sendFrame(t, bob, &webapiv1.Frame{
		Payload: &webapiv1.Frame_Cursor{Cursor: &webapiv1.CursorRequest{
			RecordId: "rec_1",
			FieldId:  "fld_1",
			Range:    "cell",
		}},
	})
	cursor := readCursor(t, alice, "cell", time.Second)
	if cursor.GetUserId() != "usr_bob" ||
		cursor.GetRecordId() != "rec_1" ||
		cursor.GetFieldId() != "fld_1" {
		t.Fatalf("cursor echo = %v, want bob at rec_1/fld_1", cursor)
	}

	sendFrame(t, bob, &webapiv1.Frame{
		Payload: &webapiv1.Frame_Selection{Selection: &webapiv1.SelectionRequest{
			Range: "A1:B2",
		}},
	})
	selection := readCursor(t, alice, "A1:B2", time.Second)
	if selection.GetUserId() != "usr_bob" {
		t.Fatalf("selection echo = %v, want bob", selection)
	}

	sendFrame(t, bob, &webapiv1.Frame{
		Payload: &webapiv1.Frame_Editing{Editing: &webapiv1.EditingRequest{
			RecordId: "rec_1",
			FieldId:  "fld_1",
			On:       true,
		}},
	})
	editing := readCursor(t, alice, "editing:on", time.Second)
	if editing.GetRecordId() != "rec_1" || editing.GetFieldId() != "fld_1" {
		t.Fatalf("editing echo = %v, want rec_1/fld_1", editing)
	}
	sendFrame(t, bob, &webapiv1.Frame{
		Payload: &webapiv1.Frame_Editing{Editing: &webapiv1.EditingRequest{
			RecordId: "rec_1",
			FieldId:  "fld_1",
			On:       false,
		}},
	})
	_ = readCursor(t, alice, "editing:off", time.Second)
}

func TestSilentClientIsRemovedAndOfflinePresenceIsBroadcast(t *testing.T) {
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: true},
		}},
		Events:           &eventStoreFake{},
		Bus:              eventbus.NewMemoryBus(),
		Now:              time.Now,
		FlushWindow:      5 * time.Millisecond,
		HeartbeatTimeout: 150 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_1")
	_ = readPresence(t, alice, "usr_bob", true, time.Second)

	stopHeartbeat := keepAlive(t, alice)
	t.Cleanup(stopHeartbeat)
	offline := readPresence(t, alice, "usr_bob", false, time.Second)
	if offline.GetUserId() != "usr_bob" {
		t.Fatalf("timeout presence = %v, want bob offline", offline)
	}
}

func TestInjectedClockDrivesHeartbeatTimeout(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: true},
		}},
		Events:           &eventStoreFake{},
		Bus:              eventbus.NewMemoryBus(),
		Now:              clock.Now,
		FlushWindow:      5 * time.Millisecond,
		HeartbeatTimeout: 5 * time.Second,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_1")
	_ = readPresence(t, alice, "usr_bob", true, time.Second)

	clock.Advance(4 * time.Second)
	sendFrame(t, alice, &webapiv1.Frame{
		MsgId:   "alice-heartbeat",
		Payload: &webapiv1.Frame_Heartbeat{Heartbeat: &webapiv1.HeartbeatRequest{}},
	})
	readAck(t, alice, "alice-heartbeat")

	clock.Advance(2 * time.Second)
	started := time.Now()
	_ = readPresence(t, alice, "usr_bob", false, 1500*time.Millisecond)
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("fake-clock timeout cleanup took %v, want supervised cleanup", elapsed)
	}
}

func TestConnectionLimitRejectsWithoutPanicking(t *testing.T) {
	if realtime.DefaultHeartbeatTimeout != 30*time.Second {
		t.Fatalf("default heartbeat timeout = %v, want 30s", realtime.DefaultHeartbeatTimeout)
	}
	if realtime.DefaultConnectionLimit != 500 {
		t.Fatalf("default connection limit = %d, want 500", realtime.DefaultConnectionLimit)
	}
	if realtime.DefaultFlushWindow != 50*time.Millisecond {
		t.Fatalf("default flush window = %v, want 50ms", realtime.DefaultFlushWindow)
	}

	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:          &eventStoreFake{},
		Bus:             eventbus.NewMemoryBus(),
		Now:             time.Now,
		ConnectionLimit: 1,
		FlushWindow:     5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	first := dialSession(t, server, "session-alice")
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	second, response, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Authorization": []string{"Bearer session-alice"},
	})
	if second != nil {
		_ = second.Close()
		t.Fatal("second websocket connection succeeded beyond the configured limit")
	}
	if response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second connection status = %v, want 429", response)
	}
	_ = response.Body.Close()
	if err == nil {
		t.Fatal("second websocket dial error = nil, want capacity rejection")
	}
	if first == nil {
		t.Fatal("first websocket connection is nil")
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(delta time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(delta)
	clock.mu.Unlock()
}

func readPresence(
	t *testing.T,
	connection *websocket.Conn,
	userID string,
	online bool,
	timeout time.Duration,
) *webapiv1.PresenceChanged {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		frame := readFrameWithin(t, connection, time.Until(deadline))
		presence := frame.GetPresence()
		if presence.GetUserId() == userID && presence.GetOnline() == online {
			return presence
		}
	}
}

func readCursor(
	t *testing.T,
	connection *websocket.Conn,
	valueRange string,
	timeout time.Duration,
) *webapiv1.CursorEcho {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		frame := readFrameWithin(t, connection, time.Until(deadline))
		cursor := frame.GetCursorEcho()
		if cursor.GetRange() == valueRange {
			return cursor
		}
	}
}

func readAck(t *testing.T, connection *websocket.Conn, msgID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		frame := readFrameWithin(t, connection, time.Until(deadline))
		if frame.GetAck().GetMsgId() == msgID {
			return
		}
	}
}

func keepAlive(t *testing.T, connection *websocket.Conn) func() {
	t.Helper()
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&webapiv1.Frame{
		Payload: &webapiv1.Frame_Heartbeat{Heartbeat: &webapiv1.HeartbeatRequest{}},
	})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}
