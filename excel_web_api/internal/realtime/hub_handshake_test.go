package realtime_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

// roomAuthorizerFake 按 (base_id, table_id) 返回房间握手裁决。
type roomAuthorizerFake struct {
	mu       sync.Mutex
	verdicts map[string]error
	calls    []string
}

func (fake *roomAuthorizerFake) AuthorizeRoom(
	_ context.Context,
	_ *commonv1.Principal,
	baseID, tableID string,
) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls = append(fake.calls, baseID+"/"+tableID)
	if fake.verdicts == nil {
		return nil
	}
	return fake.verdicts[baseID+"/"+tableID]
}

func (fake *roomAuthorizerFake) recordedCalls() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string(nil), fake.calls...)
}

// newHandshakeHub 装配一个带房间裁决 fake 的 Hub 与带 base/table 路由的测试服务器。
func newHandshakeHub(
	t *testing.T,
	authorizer realtime.RoomAuthorizer,
) (*httptest.Server, *roomAuthorizerFake) {
	t.Helper()
	fake, _ := authorizer.(*roomAuthorizerFake)
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         eventbus.NewMemoryBus(),
		Access:      authorizer,
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := httptest.NewServer(newRoutedHandler(hub))
	t.Cleanup(server.Close)
	return server, fake
}

// newRoutedHandler 复刻 cmd/main.go 的生产挂载：/bases/{base_id}/tables/{table_id}/ws。
func newRoutedHandler(hub *realtime.Hub) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/bases/{base_id}/tables/{table_id}/ws", hub.Handler())
	return mux
}

// dialRoute 以携带 Base/table 路由的地址发起 WS 握手，返回升级结果。
// 拒绝时返回 HTTP 响应（状态码可断言），成功时返回已建立的连接。
func dialRoute(
	t *testing.T,
	server *httptest.Server,
	token, baseID, tableID string,
) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/bases/" + baseID + "/tables/" + tableID + "/ws"
	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	connection, response, err := websocket.DefaultDialer.Dial(url, headers)
	if err == nil {
		t.Cleanup(func() { _ = connection.Close() })
	}
	return connection, response, err
}

// assertUpgradeRejected 断言握手未升级为 101 且返回期望状态码、不挂死。
func assertUpgradeRejected(
	t *testing.T,
	server *httptest.Server,
	token, baseID, tableID string,
	wantStatus int,
) {
	t.Helper()
	done := make(chan struct{})
	var status int
	go func() {
		defer close(done)
		connection, response, err := dialRoute(t, server, token, baseID, tableID)
		if connection != nil {
			_ = connection.Close()
		}
		if response == nil {
			status = 0
			return
		}
		status = response.StatusCode
		_ = response.Body.Close()
		_ = err
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("handshake for %s/%s hung instead of rejecting", baseID, tableID)
	}
	if status != wantStatus {
		t.Fatalf("handshake status = %d, want %d", status, wantStatus)
	}
}

// TestHandshakeRejectsUnknownBaseBeforeUpgrade 锁 TC-000-07：不存在/无权的 Base
// 在 101 升级前被拒（404/403 语义），不挂死；合法 Base 正常升级并可加入房间。
func TestHandshakeRejectsUnknownBaseBeforeUpgrade(t *testing.T) {
	authorizer := &roomAuthorizerFake{verdicts: map[string]error{
		"base_missing/table_1":  realtime.ErrRoomNotFound,
		"base_denied/table_1":   realtime.ErrRoomForbidden,
		"base_broken/table_1":   errors.New("share unavailable"),
		"base_ok/table_1":       nil,
		"base_ok/table_missing": realtime.ErrRoomNotFound,
	}}
	server, fake := newHandshakeHub(t, authorizer)

	assertUpgradeRejected(t, server, "session-alice", "base_missing", "table_1", http.StatusNotFound)
	assertUpgradeRejected(t, server, "session-alice", "base_denied", "table_1", http.StatusForbidden)
	assertUpgradeRejected(t, server, "session-alice", "base_broken", "table_1", http.StatusServiceUnavailable)
	assertUpgradeRejected(t, server, "session-alice", "base_ok", "table_missing", http.StatusNotFound)

	connection, _, err := dialRoute(t, server, "session-alice", "base_ok", "table_1")
	if err != nil {
		t.Fatalf("valid handshake rejected: %v", err)
	}
	sendFrame(t, connection, &webapiv1.Frame{
		MsgId:   "join-ok",
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  "base_ok",
			TableId: "table_1",
		}},
	})
	readSnapshot(t, connection)

	if calls := fake.recordedCalls(); len(calls) != 5 {
		t.Fatalf("authorizer calls = %v, want exactly one validation per handshake (5 dials)", calls)
	}
}

// TestHandshakeRejectsInvalidSessionBeforeUpgrade 锁会话校验语义：缺失/非法会话
// 在升级前以 401 拒绝。
func TestHandshakeRejectsInvalidSessionBeforeUpgrade(t *testing.T) {
	server, _ := newHandshakeHub(t, &roomAuthorizerFake{})

	assertUpgradeRejected(t, server, "", "base_ok", "table_1", http.StatusUnauthorized)
	assertUpgradeRejected(t, server, "session-unknown", "base_ok", "table_1", http.StatusUnauthorized)
}

// TestHandshakeWithoutRouteKeepsJoinFlow 锁裸挂载兼容语义：未携带 base/table 的
// 升级（本地测试挂载/旧客户端）不受房间裁决影响，仍可经 join 帧进房间。
func TestHandshakeWithoutRouteKeepsJoinFlow(t *testing.T) {
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         eventbus.NewMemoryBus(),
		Access:      &roomAuthorizerFake{verdicts: map[string]error{}},
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	connection := dialSession(t, server, "session-alice")
	joinRoom(t, connection, "join-bare", "base_any", "table_any")
}
