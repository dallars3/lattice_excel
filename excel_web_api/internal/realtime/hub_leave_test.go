package realtime_test

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

// frameCollector 在独立 goroutine 内持续读取一条连接的全部帧（无读超时，真实
// 客户端语义）；断言按窗口从 channel 排水统计，不在连接上反复设置读截止，
// 避免超时读破坏同一连接的后续读取。
type frameCollector struct {
	frames chan *webapiv1.Frame
	stop   chan struct{}
}

func startFrameCollector(connection *websocket.Conn) *frameCollector {
	collector := &frameCollector{
		frames: make(chan *webapiv1.Frame, 64),
		stop:   make(chan struct{}),
	}
	go func() {
		defer close(collector.frames)
		for {
			if err := connection.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
				return
			}
			_, payload, err := connection.ReadMessage()
			if err != nil {
				return
			}
			var frame webapiv1.Frame
			if err := protojson.Unmarshal(payload, &frame); err != nil {
				continue
			}
			select {
			case collector.frames <- &frame:
			case <-collector.stop:
				return
			}
		}
	}()
	return collector
}

func (collector *frameCollector) close() {
	select {
	case <-collector.stop:
	default:
		close(collector.stop)
	}
}

// countPresence 在 window 内统计某用户指定上下线状态的 presence 帧数量；
// 计数达到上限即提前返回，避免窗口空转。
func (collector *frameCollector) countPresence(
	userID string,
	online bool,
	window time.Duration,
	cap int,
) int {
	deadline := time.After(window)
	count := 0
	for {
		select {
		case frame, open := <-collector.frames:
			if !open {
				return count
			}
			presence := frame.GetPresence()
			if presence.GetUserId() == userID && presence.GetOnline() == online {
				count++
				if count >= cap {
					return count
				}
			}
		case <-deadline:
			return count
		}
	}
}

// ackWithin 在 window 内等待指定 msg_id 的 ack 帧（collector 是该连接唯一读取方）。
func (collector *frameCollector) ackWithin(msgID string, window time.Duration) bool {
	deadline := time.After(window)
	for {
		select {
		case frame, open := <-collector.frames:
			if !open {
				return false
			}
			if frame.GetAck().GetMsgId() == msgID {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// TestLeaveFrameBroadcastsOfflineExactlyOnce 锁缺陷 #18 TC-090-03 的 leave 半边：
// W-02 离开房间后房间其余成员收到一次 presence.online=false（5 秒内，实现为
// 亚秒级窗口）；重复 leave 幂等——不重复广播、不报错（ack 正常），房间可重新加入。
func TestLeaveFrameBroadcastsOfflineExactlyOnce(t *testing.T) {
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         eventbus.NewMemoryBus(),
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")
	aliceFrames := startFrameCollector(alice)
	t.Cleanup(aliceFrames.close)
	bob := dialSession(t, server, "session-bob")
	joinRoom(t, bob, "join-bob", "base_1", "table_1")

	if online := aliceFrames.countPresence("usr_bob", true, time.Second, 1); online != 1 {
		t.Fatalf("bob online frames = %d, want 1", online)
	}

	sendFrame(t, bob, &webapiv1.Frame{
		MsgId:   "leave-bob",
		Payload: &webapiv1.Frame_Leave{Leave: &webapiv1.LeaveRequest{}},
	})
	readAck(t, bob, "leave-bob")
	if offline := aliceFrames.countPresence("usr_bob", false, time.Second, 2); offline != 1 {
		t.Fatalf("offline frames after leave = %d, want exactly 1", offline)
	}

	// 重复 leave（不在房间时再离开）：ack 正常、不报错，不重复广播离场。
	sendFrame(t, bob, &webapiv1.Frame{
		MsgId:   "leave-bob-again",
		Payload: &webapiv1.Frame_Leave{Leave: &webapiv1.LeaveRequest{}},
	})
	readAck(t, bob, "leave-bob-again")
	if offline := aliceFrames.countPresence("usr_bob", false, 500*time.Millisecond, 1); offline != 0 {
		t.Fatalf("offline frames after repeated leave = %d, want 0", offline)
	}

	// 离场后可重新加入，房间恢复在线广播。
	joinRoom(t, bob, "join-bob-again", "base_1", "table_1")
	if online := aliceFrames.countPresence("usr_bob", true, time.Second, 1); online != 1 {
		t.Fatalf("rejoin online frames = %d, want 1", online)
	}
}

// TestRepeatedConnectionCloseBroadcastsSingleOffline 锁缺陷 #18 幂等半边：同一
// 用户双连接同表，第一条连接关闭不广播（成员仍在线），最后一条连接关闭恰好
// 广播一次 presence.online=false；对已关闭连接重复触发关闭不报错、不产生额外
// 离场帧，Hub 对其余连接继续可用。
func TestRepeatedConnectionCloseBroadcastsSingleOffline(t *testing.T) {
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
			"session-bob":   {UserId: "usr_bob", Internal: true},
			"session-dave":  {UserId: "usr_dave", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         eventbus.NewMemoryBus(),
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	joinRoom(t, alice, "join-alice", "base_1", "table_1")
	aliceFrames := startFrameCollector(alice)
	t.Cleanup(aliceFrames.close)

	bobFirst := dialSession(t, server, "session-bob")
	joinRoom(t, bobFirst, "join-bob-first", "base_1", "table_1")
	if online := aliceFrames.countPresence("usr_bob", true, time.Second, 1); online != 1 {
		t.Fatalf("bob first online frames = %d, want 1", online)
	}
	bobSecond := dialSession(t, server, "session-bob")
	joinRoom(t, bobSecond, "join-bob-second", "base_1", "table_1")
	if online := aliceFrames.countPresence("usr_bob", true, 500*time.Millisecond, 1); online != 0 {
		t.Fatalf("bob second online frames = %d, want 0 (same member)", online)
	}

	// 第一条连接关闭：同用户仍有第二条连接，不广播离场（本连接最后一次读取）。
	_ = bobFirst.Close()
	if offline := aliceFrames.countPresence("usr_bob", false, 500*time.Millisecond, 1); offline != 0 {
		t.Fatalf("offline frames after first close = %d, want 0", offline)
	}

	// 新观察者加入后关闭 bob 最后一条连接：恰好一次离场广播；重复关闭无第二帧。
	dave := dialSession(t, server, "session-dave")
	joinRoom(t, dave, "join-dave", "base_1", "table_1")
	daveFrames := startFrameCollector(dave)
	t.Cleanup(daveFrames.close)

	_ = bobSecond.Close()
	if offline := daveFrames.countPresence("usr_bob", false, time.Second, 2); offline != 1 {
		t.Fatalf("offline frames after final close = %d, want exactly 1", offline)
	}
	if offline := daveFrames.countPresence("usr_bob", false, 500*time.Millisecond, 1); offline != 0 {
		t.Fatalf("duplicate offline frames after repeated close = %d, want 0", offline)
	}

	// Hub 仍可服务：alice 心跳经 collector 正常应答。
	sendFrame(t, alice, &webapiv1.Frame{
		MsgId:   "alice-still-alive",
		Payload: &webapiv1.Frame_Heartbeat{Heartbeat: &webapiv1.HeartbeatRequest{}},
	})
	if !aliceFrames.ackWithin("alice-still-alive", time.Second) {
		t.Fatal("hub did not ack heartbeat after repeated close")
	}
}
