package realtime_test

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

// TestLinkClosedRevokesOnlyLinkSessionsInTargetBase 锁 issue 01：关闭链接只影响
// 该 Base 持有链接上下文的会话，收到 link_closed 后连接被关闭；内部成员与
// 其它 Base 的链接会话不受影响。
func TestLinkClosedRevokesOnlyLinkSessionsInTargetBase(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-link": {UserId: "usr_link", Internal: false, LinkContext: &commonv1.LinkContext{
				BaseId: "base_1", LinkToken: "token_1", Role: "filler",
			}},
			"session-internal": {UserId: "usr_internal", Internal: true},
			"session-other-link": {UserId: "usr_other_link", Internal: false, LinkContext: &commonv1.LinkContext{
				BaseId: "base_2", LinkToken: "token_2", Role: "viewer",
			}},
		}},
		Events:      &eventStoreFake{},
		Bus:         bus,
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	link := dialSession(t, server, "session-link")
	joinRoom(t, link, "join-link", "base_1", "table_1")
	internal := dialSession(t, server, "session-internal")
	joinRoom(t, internal, "join-internal", "base_1", "table_1")
	otherLink := dialSession(t, server, "session-other-link")
	joinRoom(t, otherLink, "join-other-link", "base_2", "table_1")

	publishedAt := time.Now()
	publishEvent(t, bus, linkClosedEvent("evt_link_closed", "base_1"))
	if revocations := readUntilClosed(t, link, "link_closed"); revocations != 1 {
		t.Fatalf("link session revocations = %d, want 1", revocations)
	}
	if elapsed := time.Since(publishedAt); elapsed >= time.Second {
		t.Fatalf("session_revoked delay = %v, want immediate push below 1s", elapsed)
	}

	assertConnectionAlive(t, internal, "internal-heartbeat")
	assertConnectionAlive(t, otherLink, "other-link-heartbeat")
}

// TestCollaboratorRemovedClosesEverySessionForTargetUser 锁 TC-090-08/W-15：
// 移除协作者后，被移除用户的全部在线连接收到 perm_changed 并被关闭；其他用户不受影响。
func TestCollaboratorRemovedClosesEverySessionForTargetUser(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice-1": {UserId: "usr_alice", Internal: true},
			"session-alice-2": {UserId: "usr_alice", Internal: true},
			"session-bob":     {UserId: "usr_bob", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         bus,
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	aliceFirst := dialSession(t, server, "session-alice-1")
	joinRoom(t, aliceFirst, "join-alice-1", "base_1", "table_1")
	aliceSecond := dialSession(t, server, "session-alice-2")
	joinRoom(t, aliceSecond, "join-alice-2", "base_1", "table_2")
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_1")

	publishEvent(t, bus, collaboratorRemovedEvent("evt_collab_removed", "base_1", "usr_alice"))

	for name, connection := range map[string]*websocket.Conn{
		"alice-1": aliceFirst,
		"alice-2": aliceSecond,
	} {
		if revocations := readUntilClosed(t, connection, "perm_changed"); revocations != 1 {
			t.Fatalf("%s revocations = %d, want 1", name, revocations)
		}
	}
	assertConnectionAlive(t, bob, "bob-heartbeat")
}

// TestRepeatedRevocationIsIdempotent 锁 issue 01：重复吊销事件不报错、不重复关闭，
// 也不影响已关闭连接；Hub 仍可继续服务其他连接。
func TestRepeatedRevocationIsIdempotent(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: true},
			"session-link": {UserId: "usr_link", Internal: false, LinkContext: &commonv1.LinkContext{
				BaseId: "base_1", LinkToken: "token_1", Role: "filler",
			}},
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
	joinRoom(t, bob, "join-bob", "base_1", "table_1")
	link := dialSession(t, server, "session-link")
	joinRoom(t, link, "join-link", "base_1", "table_1")

	publishEvent(t, bus, collaboratorRemovedEvent("evt_removed_1", "base_1", "usr_bob"))
	publishEvent(t, bus, collaboratorRemovedEvent("evt_removed_2", "base_1", "usr_bob"))
	if revocations := readUntilClosed(t, bob, "perm_changed"); revocations != 1 {
		t.Fatalf("bob revocations = %d, want exactly 1", revocations)
	}

	publishEvent(t, bus, linkClosedEvent("evt_link_closed_1", "base_1"))
	publishEvent(t, bus, linkClosedEvent("evt_link_closed_2", "base_1"))
	if revocations := readUntilClosed(t, link, "link_closed"); revocations != 1 {
		t.Fatalf("link revocations = %d, want exactly 1", revocations)
	}

	assertConnectionAlive(t, alice, "alice-heartbeat")
}

func linkClosedEvent(eventID, baseID string) eventbus.Event {
	payload, _ := json.Marshal(map[string]any{
		"base_id": baseID,
		"link_id": "link_1",
		"reason":  "force_closed",
	})
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "link.closed",
			Version:    1,
			BaseID:     baseID,
			Seq:        200,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "share",
		},
		Payload: payload,
	}
}

func collaboratorRemovedEvent(eventID, baseID, userID string) eventbus.Event {
	payload, _ := json.Marshal(map[string]any{
		"base_id":     baseID,
		"change_type": "removed",
		"user_id":     userID,
	})
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "share.changed",
			Version:    1,
			BaseID:     baseID,
			Seq:        0,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "share",
		},
		Payload: payload,
	}
}

// readUntilClosed 读取帧直到连接被服务端关闭，返回收到的吊销帧数量；
// 若只是读超时（连接未关闭）则判失败。
func readUntilClosed(t *testing.T, connection *websocket.Conn, reason string) int {
	t.Helper()
	revocations := 0
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := connection.SetReadDeadline(deadline); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, payload, err := connection.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				t.Fatalf("connection stayed open without close after revocation: %v", err)
			}
			return revocations
		}
		var frame webapiv1.Frame
		if err := protojson.Unmarshal(payload, &frame); err != nil {
			t.Fatalf("unmarshal frame %s: %v", payload, err)
		}
		revoked := frame.GetSessionRevoked()
		if revoked == nil {
			continue
		}
		if revoked.GetReason() != reason {
			t.Fatalf("session_revoked reason = %q, want %q", revoked.GetReason(), reason)
		}
		revocations++
	}
}

func assertConnectionAlive(t *testing.T, connection *websocket.Conn, msgID string) {
	t.Helper()
	sendFrame(t, connection, &webapiv1.Frame{
		MsgId:   msgID,
		Payload: &webapiv1.Frame_Heartbeat{Heartbeat: &webapiv1.HeartbeatRequest{}},
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		frame := readFrameWithin(t, connection, time.Until(deadline))
		if frame.GetSessionRevoked() != nil {
			t.Fatalf("unrelated connection received session_revoked: %v", frame.GetSessionRevoked())
		}
		if frame.GetAck().GetMsgId() == msgID {
			return
		}
	}
}
