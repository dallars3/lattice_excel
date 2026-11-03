package realtime_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

func TestHandlerUsesMountedPathIDsForJoinValidation(t *testing.T) {
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:      &eventStoreFake{},
		Bus:         eventbus.NewMemoryBus(),
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	mux := http.NewServeMux()
	mux.Handle("/bases/{base_id}/tables/{table_id}/ws", hub.Handler())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/bases/base_1/tables/table_1/ws"
	connection, _, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Cookie": []string{"sid=session-alice"},
	})
	if err != nil {
		t.Fatalf("websocket dial with cookie error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	sendFrame(t, connection, &webapiv1.Frame{
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  "base_2",
			TableId: "table_1",
		}},
	})
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := connection.ReadMessage()
	if err != nil {
		return
	}
	var frame webapiv1.Frame
	if unmarshalErr := protojson.Unmarshal(payload, &frame); unmarshalErr != nil {
		t.Fatalf("unmarshal frame: %v", unmarshalErr)
	}
	if frame.GetSnapshot() != nil {
		t.Fatal("join conflicting with mounted path received a snapshot")
	}
}
