// realtime/client.go：单连接读写、心跳和发送队列。
package realtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
)

type client struct {
	hub          *Hub
	connection   *websocket.Conn
	principal    *commonv1.Principal
	member       principalMember
	routeBaseID  string
	routeTableID string

	send chan *webapiv1.Frame
	done chan struct{}

	mu       sync.Mutex
	room     *room
	lastSeq  int64
	lastRead time.Time

	// closeAfterFlush 标记写协程：吊销帧写出后关闭连接，避免 close() 抢先
	// 关闭底层连接而丢掉队列中的 session_revoked 帧。
	closeAfterFlush bool

	closeOnce sync.Once
}

func (client *client) touch() {
	client.mu.Lock()
	client.lastRead = client.hub.now()
	client.mu.Unlock()
}

func (client *client) expired(now time.Time, timeout time.Duration) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return now.Sub(client.lastRead) >= timeout
}

func newClient(
	hub *Hub,
	connection *websocket.Conn,
	principal *commonv1.Principal,
	palette []string,
	baseID, tableID string,
) *client {
	return &client{
		hub:          hub,
		connection:   connection,
		principal:    principal,
		member:       memberFromPrincipal(principal, palette),
		routeBaseID:  baseID,
		routeTableID: tableID,
		send:         make(chan *webapiv1.Frame, hub.maxBatch*4),
		done:         make(chan struct{}),
		lastRead:     hub.now(),
	}
}

func (client *client) enqueue(frame *webapiv1.Frame) {
	if frame == nil {
		return
	}
	select {
	case client.send <- frame:
	case <-client.done:
	default:
		client.close()
	}
}

// revoke 投递吊销帧并在该帧刷出后关闭连接。重复调用幂等：只投递一次、
// 只触发一次关闭；队列已满或连接已关闭时沿用 enqueue/close 的既有语义。
func (client *client) revoke(frame *webapiv1.Frame) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closeAfterFlush {
		return
	}
	client.closeAfterFlush = true
	client.enqueue(frame)
}

func (client *client) shouldCloseAfterFlush() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.closeAfterFlush && len(client.send) == 0
}

func (client *client) writePump() {
	defer client.hub.wait.Done()
	for {
		var first *webapiv1.Frame
		select {
		case first = <-client.send:
		case <-client.done:
			return
		}
		batch := []*webapiv1.Frame{first}
		timer := time.NewTimer(client.hub.flushWindow)
	collect:
		for len(batch) < client.hub.maxBatch {
			select {
			case frame := <-client.send:
				batch = append(batch, frame)
			case <-timer.C:
				break collect
			case <-client.done:
				timer.Stop()
				return
			}
		}
		timer.Stop()
		if err := client.writeBatch(batch); err != nil {
			client.close()
			return
		}
		if client.shouldCloseAfterFlush() {
			client.close()
			return
		}
	}
}

func (client *client) writeBatch(batch []*webapiv1.Frame) error {
	if err := client.connection.SetWriteDeadline(
		time.Now().Add(client.hub.clientHeartbeatInterval),
	); err != nil {
		return err
	}
	for _, frame := range batch {
		payload, err := marshalFrame(frame)
		if err != nil {
			return err
		}
		if err := client.connection.WriteMessage(websocket.TextMessage, payload); err != nil {
			return err
		}
	}
	return nil
}

func (client *client) readPump(ctx context.Context) {
	defer client.hub.removeClient(client)
	if err := client.connection.SetReadDeadline(
		time.Now().Add(client.hub.heartbeatTimeout),
	); err != nil {
		return
	}
	for {
		messageType, payload, err := client.connection.ReadMessage()
		if err != nil {
			return
		}
		client.touch()
		if err := client.connection.SetReadDeadline(
			time.Now().Add(client.hub.heartbeatTimeout),
		); err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		frame, err := unmarshalFrame(payload)
		if err != nil {
			client.close()
			return
		}
		if err := client.hub.dispatch(ctx, client, frame); err != nil {
			client.close()
			return
		}
	}
}

func (client *client) sendAck(msgID string) {
	if msgID == "" {
		return
	}
	client.enqueue(&webapiv1.Frame{
		Payload: &webapiv1.Frame_Ack{Ack: &webapiv1.Ack{MsgId: msgID}},
	})
}

func (client *client) close() {
	client.closeOnce.Do(func() {
		close(client.done)
		_ = client.connection.Close()
	})
}

var errInvalidMessage = errors.New("invalid realtime frame")
