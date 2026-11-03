package realtime_test

import (
	"testing"
	"time"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

func TestReplayRequestReplaysDurableIncrementWithoutDuplicates(t *testing.T) {
	store := &eventStoreFake{}
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
	store.mu.Lock()
	store.events = []*webapiv1.Frame{
		cellChangedFrame(11, "rec_1", "fld_1", "usr_bob"),
		cellChangedFrame(11, "rec_1", "fld_1", "usr_bob"),
		cellChangedFrame(12, "rec_1", "fld_2", "usr_bob"),
	}
	store.mu.Unlock()

	sendFrame(t, alice, &webapiv1.Frame{
		MsgId: "replay-1",
		Payload: &webapiv1.Frame_ReplayRequest{ReplayRequest: &webapiv1.ReplayRequest{
			LastSeq: 10,
		}},
	})

	sequences := make([]int64, 0, 2)
	for {
		frame := readFrameWithin(t, alice, time.Second)
		if changed := frame.GetCellChanged(); changed != nil {
			sequences = append(sequences, changed.GetSeq())
		}
		if frame.GetAck().GetMsgId() == "replay-1" {
			break
		}
		if frame.GetSnapshot() != nil {
			t.Fatalf("replay_request unexpectedly returned snapshot: %v", frame.GetSnapshot())
		}
	}
	if len(sequences) != 2 || sequences[0] != 11 || sequences[1] != 12 {
		t.Fatalf("replayed sequences = %v, want [11 12]", sequences)
	}
}
