package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

type sessionFake struct {
	principals map[string]*commonv1.Principal
}

func (fake *sessionFake) ExchangeSession(
	_ context.Context,
	request *iamv1.ExchangeSessionRequest,
) (*iamv1.ExchangeSessionResponse, error) {
	principal, ok := fake.principals[request.GetToken()]
	if !ok {
		return nil, errors.New("unknown session")
	}
	return &iamv1.ExchangeSessionResponse{Principal: principal}, nil
}

type eventStoreFake struct {
	mu     sync.Mutex
	events []*webapiv1.Frame
	calls  []eventStoreCall
}

type pagedEventStoreFake struct {
	events []*webapiv1.Frame
}

func (store *pagedEventStoreFake) Since(
	_ context.Context,
	_ string,
	_ string,
	lastSeq int64,
	limit int,
) ([]*webapiv1.Frame, error) {
	events := make([]*webapiv1.Frame, 0, limit)
	for _, event := range store.events {
		if frameSequence(event) > lastSeq && len(events) < limit {
			events = append(events, event)
		}
	}
	return events, nil
}

type eventStoreCall struct {
	baseID  string
	tableID string
	lastSeq int64
	limit   int
}

func (store *eventStoreFake) Since(
	_ context.Context,
	baseID, tableID string,
	lastSeq int64,
	limit int,
) ([]*webapiv1.Frame, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls = append(store.calls, eventStoreCall{
		baseID:  baseID,
		tableID: tableID,
		lastSeq: lastSeq,
		limit:   limit,
	})
	events := make([]*webapiv1.Frame, 0, len(store.events))
	for _, event := range store.events {
		if frameSequence(event) > lastSeq {
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(left, right int) bool {
		return frameSequence(events[left]) < frameSequence(events[right])
	})
	return events, nil
}

func frameSequence(frame *webapiv1.Frame) int64 {
	switch payload := frame.GetPayload().(type) {
	case *webapiv1.Frame_CellChanged:
		return payload.CellChanged.GetSeq()
	case *webapiv1.Frame_RecordDeleted:
		return payload.RecordDeleted.GetSeq()
	case *webapiv1.Frame_StructureChanged:
		return payload.StructureChanged.GetSeq()
	default:
		return 0
	}
}

func TestJoinReplaysIncrementThenReturnsSnapshotWithoutDuplicates(t *testing.T) {
	store := &eventStoreFake{events: []*webapiv1.Frame{
		cellChangedFrame(101, "rec_1", "fld_1", "usr_int"),
		cellChangedFrame(102, "rec_2", "fld_2", "usr_other"),
		cellChangedFrame(101, "rec_1", "fld_1", "usr_int"),
	}}
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-internal": {UserId: "usr_int", Internal: true},
			"session-external": {UserId: "usr_ext", Internal: false},
		}},
		Events: store,
		Bus:    eventbus.NewMemoryBus(),
		Now:    func() time.Time { return time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC) },
	})
	startHub(t, hub)
	server := httptest.NewServer(hub.Handler())
	t.Cleanup(server.Close)

	internal := dialSession(t, server, "session-internal")
	sendFrame(t, internal, &webapiv1.Frame{
		MsgId: "join-1",
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  "base_1",
			TableId: "table_1",
			LastSeq: 100,
		}},
	})

	first := readFrame(t, internal)
	second := readFrame(t, internal)
	snapshot := readFrame(t, internal)
	if sequence := frameSequence(first); sequence != 101 {
		t.Fatalf("first replay seq = %d, want 101", sequence)
	}
	if sequence := frameSequence(second); sequence != 102 {
		t.Fatalf("second replay seq = %d, want 102", sequence)
	}
	if snapshot.GetSnapshot() == nil || snapshot.GetSnapshot().GetSeq() != 102 {
		t.Fatalf("snapshot = %v, want seq 102", snapshot)
	}
	if len(snapshot.GetSnapshot().GetMemberList()) != 1 {
		t.Fatalf("member count = %d, want 1", len(snapshot.GetSnapshot().GetMemberList()))
	}
	member := snapshot.GetSnapshot().GetMemberList()[0]
	if member.GetUserId() != "usr_int" || !member.GetOnline() || !member.GetInternal() ||
		member.GetColor() == "" {
		t.Fatalf("internal member = %v, want online internal with stable color", member)
	}

	external := dialSession(t, server, "session-external")
	sendFrame(t, external, &webapiv1.Frame{
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  "base_1",
			TableId: "table_1",
			LastSeq: 102,
		}},
	})
	externalSnapshot := readFrame(t, external)
	members := externalSnapshot.GetSnapshot().GetMemberList()
	if len(members) != 2 {
		t.Fatalf("member count = %d, want internal and external members", len(members))
	}
	byUser := make(map[string]*webapiv1.MemberPresence, len(members))
	for _, item := range members {
		byUser[item.GetUserId()] = item
	}
	if !byUser["usr_int"].GetInternal() || byUser["usr_ext"].GetInternal() {
		t.Fatalf("member internal labels = internal:%v external:%v",
			byUser["usr_int"].GetInternal(), byUser["usr_ext"].GetInternal())
	}

	store.mu.Lock()
	calls := append([]eventStoreCall(nil), store.calls...)
	store.mu.Unlock()
	if len(calls) == 0 || calls[0].baseID != "base_1" || calls[0].tableID != "table_1" ||
		calls[0].lastSeq != 100 || calls[0].limit != 15 {
		t.Fatalf("first EventStore.Since call = %+v, want base_1/table_1/100/15", calls[0])
	}
}

func TestJoinReplaysEveryPageAndKeepsDistinctFramesAtOneSequence(t *testing.T) {
	events := make([]*webapiv1.Frame, 0, 21)
	for seq := int64(11); seq <= 28; seq++ {
		events = append(events, cellChangedFrame(seq, "rec_1", "fld_1", "usr_other"))
	}
	events = append(events,
		&webapiv1.Frame{Payload: &webapiv1.Frame_RecordDeleted{
			RecordDeleted: &webapiv1.RecordDeleted{Seq: 29, RecordId: "rec_1"},
		}},
		&webapiv1.Frame{Payload: &webapiv1.Frame_RecordDeleted{
			RecordDeleted: &webapiv1.RecordDeleted{Seq: 29, RecordId: "rec_2"},
		}},
		&webapiv1.Frame{Payload: &webapiv1.Frame_RecordDeleted{
			RecordDeleted: &webapiv1.RecordDeleted{Seq: 29, RecordId: "rec_1"},
		}},
	)
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:      &pagedEventStoreFake{events: events},
		Bus:         eventbus.NewMemoryBus(),
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
		MaxBatch:    15,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	sendFrame(t, alice, &webapiv1.Frame{
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  "base_1",
			TableId: "table_1",
			LastSeq: 10,
		}},
	})

	replayed := 0
	deleted := make(map[string]int)
	var snapshot *webapiv1.Snapshot
	for snapshot == nil {
		frame := readFrameWithin(t, alice, 2*time.Second)
		switch {
		case frame.GetCellChanged() != nil:
			replayed++
		case frame.GetRecordDeleted() != nil:
			replayed++
			deleted[frame.GetRecordDeleted().GetRecordId()]++
		case frame.GetSnapshot() != nil:
			snapshot = frame.GetSnapshot()
		}
	}
	if replayed != 20 {
		t.Fatalf("replayed frame count = %d, want 20 across all pages", replayed)
	}
	if deleted["rec_1"] != 1 || deleted["rec_2"] != 1 {
		t.Fatalf("deleted replay counts = %v, want one each", deleted)
	}
	if snapshot.GetSeq() != 29 {
		t.Fatalf("snapshot seq = %d, want 29", snapshot.GetSeq())
	}
}

func startHub(t *testing.T, hub *realtime.Hub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := hub.Start(ctx); err != nil {
		t.Fatalf("Hub.Start() error = %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		if err := hub.Close(closeCtx); err != nil {
			t.Errorf("Hub.Close() error = %v", err)
		}
	})
}

func dialSession(t *testing.T, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	connection, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatalf("websocket dial error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

func sendFrame(t *testing.T, connection *websocket.Conn, frame *webapiv1.Frame) {
	t.Helper()
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func readFrame(t *testing.T, connection *websocket.Conn) *webapiv1.Frame {
	t.Helper()
	return readFrameWithin(t, connection, 2*time.Second)
}

func readFrameWithin(
	t *testing.T,
	connection *websocket.Conn,
	timeout time.Duration,
) *webapiv1.Frame {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := connection.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatalf("frame is not JSON: %s", payload)
	}
	var frame webapiv1.Frame
	if err := protojson.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("unmarshal frame %s: %v", payload, err)
	}
	return &frame
}

func cellChangedFrame(seq int64, recordID, fieldID, operator string) *webapiv1.Frame {
	return &webapiv1.Frame{Payload: &webapiv1.Frame_CellChanged{
		CellChanged: &webapiv1.CellChanged{
			Seq:      seq,
			RecordId: recordID,
			FieldId:  fieldID,
			Operator: operator,
		},
	}}
}
