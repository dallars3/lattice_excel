// realtime/hub.go：WebSocket Hub 生命周期、鉴权、接入与消息分派。
package realtime

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	webapiv1 "excel/api/webapi/v1"
)

// Hub owns pure in-memory rooms and shared event subscriptions.
type Hub struct {
	session SessionResolver
	events  EventStore
	bus     eventbus.EventBus
	now     func() time.Time

	heartbeatTimeout        time.Duration
	connectionLimit         int
	flushWindow             time.Duration
	maxBatch                int
	clientHeartbeatInterval time.Duration
	palette                 []string
	upgrader                websocket.Upgrader

	mu      sync.Mutex
	rooms   map[roomKey]*room
	clients map[*client]struct{}
	baseSeq map[string]int64
	started bool
	closed  bool
	cancel  context.CancelFunc
	wait    sync.WaitGroup
}

// NewHub creates a realtime hub. Start must be called before serving traffic.
func NewHub(deps HubDeps) *Hub {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	palette := append([]string(nil), deps.Colors...)
	if len(palette) == 0 {
		palette = append([]string(nil), defaultPalette...)
	}
	hub := &Hub{
		session:          deps.Session,
		events:           deps.Events,
		bus:              deps.Bus,
		now:              now,
		heartbeatTimeout: positiveDuration(deps.HeartbeatTimeout, DefaultHeartbeatTimeout),
		connectionLimit:  positiveInt(deps.ConnectionLimit, DefaultConnectionLimit),
		flushWindow:      positiveDuration(deps.FlushWindow, DefaultFlushWindow),
		maxBatch:         positiveInt(deps.MaxBatch, DefaultMaxBatch),
		clientHeartbeatInterval: positiveDuration(
			deps.ClientHeartbeatInterval,
			DefaultClientHeartbeatInterval,
		),
		palette: palette,
		rooms:   make(map[roomKey]*room),
		clients: make(map[*client]struct{}),
		baseSeq: make(map[string]int64),
	}
	hub.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     sameOrigin,
	}
	return hub
}

// Handler returns the HTTP wrapper around the WebSocket upgrade endpoint.
func (hub *Hub) Handler() http.Handler {
	return http.HandlerFunc(hub.serveHTTP)
}

// Start begins shared event subscriptions and background supervision.
func (hub *Hub) Start(ctx context.Context) error {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return errors.New("realtime hub is closed")
	}
	if hub.started {
		return errors.New("realtime hub already started")
	}
	if hub.session == nil {
		return errors.New("session resolver is required")
	}
	if hub.bus == nil {
		return errors.New("event bus is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	if err := hub.startSubscriptions(runCtx); err != nil {
		cancel()
		return err
	}
	hub.started = true
	hub.cancel = cancel
	return nil
}

// Close terminates subscriptions and active WebSocket connections.
func (hub *Hub) Close(ctx context.Context) error {
	hub.mu.Lock()
	if hub.closed {
		hub.mu.Unlock()
		return nil
	}
	hub.closed = true
	cancel := hub.cancel
	clients := make([]*client, 0, len(hub.clients))
	for client := range hub.clients {
		clients = append(clients, client)
	}
	hub.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, client := range clients {
		client.close()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		hub.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (hub *Hub) serveHTTP(response http.ResponseWriter, request *http.Request) {
	hub.mu.Lock()
	closed := hub.closed || !hub.started
	atCapacity := len(hub.clients) >= hub.connectionLimit
	hub.mu.Unlock()
	if closed {
		http.Error(response, "realtime unavailable", http.StatusServiceUnavailable)
		return
	}
	if atCapacity {
		http.Error(response, "realtime capacity reached", http.StatusTooManyRequests)
		return
	}

	principal, err := hub.resolvePrincipal(request)
	if err != nil {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return
	}
	hub.mu.Lock()
	if hub.closed || !hub.started || len(hub.clients) >= hub.connectionLimit {
		hub.mu.Unlock()
		http.Error(response, "realtime capacity reached", http.StatusTooManyRequests)
		return
	}
	connection, err := hub.upgrader.Upgrade(response, request, nil)
	if err != nil {
		hub.mu.Unlock()
		return
	}
	client := newClient(
		hub,
		connection,
		principal,
		hub.palette,
		routeValue(request, "base_id"),
		routeValue(request, "table_id"),
	)
	hub.clients[client] = struct{}{}
	hub.wait.Add(1)
	hub.mu.Unlock()

	go client.writePump()
	client.readPump(request.Context())
}

func (hub *Hub) resolvePrincipal(request *http.Request) (*commonv1.Principal, error) {
	token := sessionToken(request)
	if token == "" {
		return nil, errors.New("session token is missing")
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	result, err := hub.session.ExchangeSession(ctx, &iamv1.ExchangeSessionRequest{Token: token})
	if err != nil {
		return nil, err
	}
	principal := result.GetPrincipal()
	if principal.GetUserId() == "" {
		return nil, errors.New("session principal is empty")
	}
	return principal, nil
}

func (hub *Hub) dispatch(ctx context.Context, client *client, frame *webapiv1.Frame) error {
	switch payload := frame.GetPayload().(type) {
	case *webapiv1.Frame_Join:
		if err := hub.join(ctx, client, payload.Join); err != nil {
			return err
		}
	case *webapiv1.Frame_Leave:
		hub.leave(client)
	case *webapiv1.Frame_Heartbeat:
		client.touch()
	case *webapiv1.Frame_Cursor:
		hub.forwardCursor(client, &webapiv1.CursorEcho{
			UserId:   client.principal.GetUserId(),
			RecordId: payload.Cursor.GetRecordId(),
			FieldId:  payload.Cursor.GetFieldId(),
			Range:    payload.Cursor.GetRange(),
		})
	case *webapiv1.Frame_Selection:
		hub.forwardCursor(client, &webapiv1.CursorEcho{
			UserId: client.principal.GetUserId(),
			Range:  payload.Selection.GetRange(),
		})
	case *webapiv1.Frame_Editing:
		state := "editing:off"
		if payload.Editing.GetOn() {
			state = "editing:on"
		}
		hub.forwardCursor(client, &webapiv1.CursorEcho{
			UserId:   client.principal.GetUserId(),
			RecordId: payload.Editing.GetRecordId(),
			FieldId:  payload.Editing.GetFieldId(),
			Range:    state,
		})
	case *webapiv1.Frame_ReplayRequest:
		if err := hub.replay(ctx, client, payload.ReplayRequest.GetLastSeq()); err != nil {
			return err
		}
	default:
		if frame.GetPayload() == nil {
			return errInvalidMessage
		}
	}
	client.sendAck(frame.GetMsgId())
	return nil
}

func (hub *Hub) join(ctx context.Context, client *client, request *webapiv1.JoinRequest) error {
	if request == nil {
		return errInvalidMessage
	}
	baseID := strings.TrimSpace(request.GetBaseId())
	tableID := strings.TrimSpace(request.GetTableId())
	if baseID == "" || tableID == "" {
		return errInvalidMessage
	}
	if client.routeBaseID != "" && client.routeBaseID != baseID {
		return errInvalidMessage
	}
	if client.routeTableID != "" && client.routeTableID != tableID {
		return errInvalidMessage
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return errors.New("realtime hub is closed")
	}
	if client.room != nil {
		hub.leaveRoomLocked(client)
	}
	key := roomKey{baseID: baseID, tableID: tableID}
	currentRoom := hub.rooms[key]
	if currentRoom == nil {
		currentRoom = newRoom(key)
		hub.rooms[key] = currentRoom
	}

	var events []*webapiv1.Frame
	if request.GetLastSeq() > 0 {
		var err error
		events, err = hub.replayLocked(ctx, baseID, tableID, request.GetLastSeq())
		if err != nil {
			return err
		}
	}
	client.room = currentRoom
	client.lastSeq = request.GetLastSeq()
	if currentRoom.add(client) {
		hub.broadcastRoomLocked(
			currentRoom,
			client,
			presenceFrame(client.member, true),
		)
	}
	for _, event := range replayableEvents(events, client.lastSeq) {
		sequence := frameSequence(event)
		if sequence > client.lastSeq {
			client.lastSeq = sequence
		}
		client.enqueue(event)
	}
	if client.lastSeq > hub.baseSeq[baseID] {
		hub.baseSeq[baseID] = client.lastSeq
	}
	client.enqueue(&webapiv1.Frame{Payload: &webapiv1.Frame_Snapshot{
		Snapshot: &webapiv1.Snapshot{
			Seq:        hub.baseSeq[baseID],
			MemberList: currentRoom.presenceList(),
		},
	}})
	return nil
}

func (hub *Hub) replay(
	ctx context.Context,
	client *client,
	lastSeq int64,
) error {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if client.room == nil {
		return errInvalidMessage
	}
	events, err := hub.replayLocked(
		ctx,
		client.room.key.baseID,
		client.room.key.tableID,
		lastSeq,
	)
	if err != nil {
		return err
	}
	for _, event := range replayableEvents(events, maxInt64(lastSeq, client.lastSeq)) {
		sequence := frameSequence(event)
		client.enqueue(event)
		if sequence > client.lastSeq {
			client.lastSeq = sequence
		}
	}
	if client.lastSeq > hub.baseSeq[client.room.key.baseID] {
		hub.baseSeq[client.room.key.baseID] = client.lastSeq
	}
	return nil
}

func (hub *Hub) replayLocked(
	ctx context.Context,
	baseID, tableID string,
	lastSeq int64,
) ([]*webapiv1.Frame, error) {
	if hub.events == nil {
		return nil, errors.New("event store is required")
	}
	events := make([]*webapiv1.Frame, 0)
	cursor := lastSeq
	for {
		page, err := hub.events.Since(ctx, baseID, tableID, cursor, hub.maxBatch)
		if err != nil {
			return nil, err
		}
		events = append(events, page...)
		if len(page) < hub.maxBatch {
			break
		}
		nextCursor := cursor
		for _, event := range page {
			if sequence := frameSequence(event); sequence > nextCursor {
				nextCursor = sequence
			}
		}
		if nextCursor <= cursor {
			break
		}
		cursor = nextCursor
	}
	return events, nil
}

func replayableEvents(events []*webapiv1.Frame, lastSeq int64) []*webapiv1.Frame {
	sorted := append([]*webapiv1.Frame(nil), events...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return frameSequence(sorted[left]) < frameSequence(sorted[right])
	})
	filtered := make([]*webapiv1.Frame, 0, len(sorted))
	seen := make(map[string]struct{}, len(sorted))
	for _, event := range sorted {
		if !isReplayableFrame(event) {
			continue
		}
		sequence := frameSequence(event)
		if sequence > 0 && sequence <= lastSeq {
			continue
		}
		key := frameDedupeKey(event)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, event)
	}
	return filtered
}

func frameDedupeKey(frame *webapiv1.Frame) string {
	payload, err := marshalFrame(frame)
	if err != nil {
		return frame.String()
	}
	return string(payload)
}

func isReplayableFrame(frame *webapiv1.Frame) bool {
	switch frame.GetPayload().(type) {
	case *webapiv1.Frame_CellChanged,
		*webapiv1.Frame_RecordDeleted,
		*webapiv1.Frame_StructureChanged:
		return true
	default:
		return false
	}
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func (hub *Hub) removeClient(client *client) {
	hub.mu.Lock()
	if _, exists := hub.clients[client]; exists {
		delete(hub.clients, client)
		if client.room != nil && client.room.remove(client) && !hub.closed {
			hub.broadcastRoomLocked(
				client.room,
				client,
				presenceFrame(client.member, false),
			)
		}
		client.room = nil
		for key, currentRoom := range hub.rooms {
			if len(currentRoom.clients) == 0 {
				delete(hub.rooms, key)
			}
		}
	}
	hub.mu.Unlock()
	client.close()
}

func (hub *Hub) leave(client *client) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if client.room == nil {
		return
	}
	currentRoom := client.room
	if currentRoom.remove(client) {
		hub.broadcastRoomLocked(
			currentRoom,
			client,
			presenceFrame(client.member, false),
		)
	}
	client.room = nil
	if len(currentRoom.clients) == 0 {
		delete(hub.rooms, currentRoom.key)
	}
}

func (hub *Hub) leaveRoomLocked(client *client) {
	if client.room == nil {
		return
	}
	currentRoom := client.room
	if currentRoom.remove(client) {
		hub.broadcastRoomLocked(
			currentRoom,
			client,
			presenceFrame(client.member, false),
		)
	}
	client.room = nil
	if len(currentRoom.clients) == 0 {
		delete(hub.rooms, currentRoom.key)
	}
}

func (hub *Hub) forwardCursor(source *client, cursor *webapiv1.CursorEcho) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if source.room == nil {
		return
	}
	frame := &webapiv1.Frame{Payload: &webapiv1.Frame_CursorEcho{CursorEcho: cursor}}
	hub.broadcastRoomLocked(source.room, source, frame)
}

func (hub *Hub) broadcastRoomLocked(currentRoom *room, source *client, frame *webapiv1.Frame) {
	for candidate := range currentRoom.clients {
		if candidate == source {
			continue
		}
		candidate.enqueue(frame)
	}
}

func presenceFrame(member principalMember, online bool) *webapiv1.Frame {
	return &webapiv1.Frame{Payload: &webapiv1.Frame_Presence{
		Presence: &webapiv1.PresenceChanged{
			UserId:   member.userID,
			Color:    member.color,
			Online:   online,
			Internal: member.internal,
		},
	}}
}

func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return strings.EqualFold(origin, "http://"+request.Host) ||
		strings.EqualFold(origin, "https://"+request.Host)
}

func sessionToken(request *http.Request) string {
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(authorization) > len("Bearer ") &&
		strings.EqualFold(authorization[:len("Bearer ")], "Bearer ") {
		return strings.TrimSpace(authorization[len("Bearer "):])
	}
	if cookie, err := request.Cookie("sid"); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func routeValue(request *http.Request, name string) string {
	if value := strings.TrimSpace(request.URL.Query().Get(name)); value != "" {
		return value
	}
	return strings.TrimSpace(request.PathValue(name))
}
