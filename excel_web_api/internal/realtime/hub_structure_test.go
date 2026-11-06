package realtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	webapiv1 "excel/api/webapi/v1"
	"excel_web_api/internal/realtime"
)

// 缺陷 #18（W-11/W-07）：结构变更进入持久事件流后，断线重连必须与单元格变更
// 一样补齐——join.last_seq 与 replay_request 等价，帧去重、seq 单调，快照 seq
// 与后续在线增量衔接。
func structureChangedFrame(seq int64, changeType, tableID, fieldID string) *webapiv1.Frame {
	return &webapiv1.Frame{Payload: &webapiv1.Frame_StructureChanged{
		StructureChanged: &webapiv1.StructureChanged{
			Seq:        seq,
			ChangeType: changeType,
			TableId:    tableID,
			FieldId:    fieldID,
		},
	}}
}

func TestJoinReplaysStructureEventsAndSnapshotConnectsToLiveIncrement(t *testing.T) {
	bus := eventbus.NewMemoryBus()
	store := &eventStoreFake{events: []*webapiv1.Frame{
		structureChangedFrame(51, "field_created", "table_1", "fld_new"),
		cellChangedFrame(52, "rec_1", "fld_1", "usr_bob"),
		structureChangedFrame(51, "field_created", "table_1", "fld_new"),
		structureChangedFrame(53, "view_created", "table_1", ""),
	}}
	hub := realtime.NewHub(realtime.HubDeps{
		Session: &sessionFake{principals: map[string]*commonv1.Principal{
			"session-alice": {UserId: "usr_alice", Internal: true},
		}},
		Events:      store,
		Bus:         bus,
		Now:         time.Now,
		FlushWindow: 5 * time.Millisecond,
	})
	startHub(t, hub)
	server := newRealtimeServer(t, hub)

	alice := dialSession(t, server, "session-alice")
	sendFrame(t, alice, &webapiv1.Frame{
		MsgId: "join-1",
		Payload: &webapiv1.Frame_Join{Join: &webapiv1.JoinRequest{
			BaseId:  "base_1",
			TableId: "table_1",
			LastSeq: 50,
		}},
	})

	type replayedFrame struct {
		seq     int64
		kind    string
		payload *webapiv1.StructureChanged
	}
	replayed := make([]replayedFrame, 0, 3)
	var snapshot *webapiv1.Snapshot
	for snapshot == nil {
		frame := readFrameWithin(t, alice, time.Second)
		switch {
		case frame.GetStructureChanged() != nil:
			changed := frame.GetStructureChanged()
			replayed = append(replayed, replayedFrame{
				seq:     changed.GetSeq(),
				kind:    changed.GetChangeType(),
				payload: changed,
			})
		case frame.GetCellChanged() != nil:
			replayed = append(replayed, replayedFrame{seq: frame.GetCellChanged().GetSeq()})
		case frame.GetSnapshot() != nil:
			snapshot = frame.GetSnapshot()
		}
	}
	if len(replayed) != 3 ||
		replayed[0].seq != 51 || replayed[0].kind != "field_created" ||
		replayed[0].payload.GetTableId() != "table_1" ||
		replayed[0].payload.GetFieldId() != "fld_new" ||
		replayed[1].seq != 52 ||
		replayed[2].seq != 53 || replayed[2].kind != "view_created" {
		t.Fatalf("replayed frames = %+v, want 51 field_created / 52 cell / 53 view_created", replayed)
	}
	if snapshot.GetSeq() != 53 {
		t.Fatalf("snapshot seq = %d, want 53 (no gap after replay)", snapshot.GetSeq())
	}

	// 快照后的在线结构增量继续衔接（seq 单调、不出现缺口）。
	publishStructureEvent(t, bus, 54, "field_renamed", "table_1", "fld_new", "usr_bob")
	for {
		frame := readFrameWithin(t, alice, time.Second)
		if changed := frame.GetStructureChanged(); changed != nil {
			if changed.GetSeq() != 54 || changed.GetChangeType() != "field_renamed" {
				t.Fatalf("live structure frame = %v, want seq 54 field_renamed", changed)
			}
			return
		}
	}
}

// publishStructureEvent 模拟 core 结构写发布的 record.changed 结构事件。
func publishStructureEvent(
	t *testing.T,
	bus *eventbus.MemoryBus,
	seq int64,
	changeType, tableID, fieldID, operator string,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"table_id":    tableID,
		"field_id":    fieldID,
		"change_type": changeType,
		"operator":    operator,
	})
	if err != nil {
		t.Fatalf("marshal structure event payload: %v", err)
	}
	if err := bus.Publish(context.Background(), eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    fmt.Sprintf("evt_structure_%d", seq),
			Type:       "record.changed",
			Version:    1,
			BaseID:     "base_1",
			Seq:        seq,
			OccurredAt: time.Now().UnixMilli(),
			Producer:   "structure",
		},
		Payload: payload,
	}); err != nil {
		t.Fatalf("publish structure event: %v", err)
	}
}

func TestReplayRequestDeliversStructureIncrementsWithoutDuplicates(t *testing.T) {
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
		structureChangedFrame(61, "field_created", "table_1", "fld_1"),
		structureChangedFrame(62, "field_created", "table_1", "fld_2"),
		structureChangedFrame(61, "field_created", "table_1", "fld_1"),
		cellChangedFrame(62, "rec_1", "fld_1", "usr_bob"),
		structureChangedFrame(63, "table_renamed", "table_1", ""),
	}
	store.mu.Unlock()

	sendFrame(t, alice, &webapiv1.Frame{
		MsgId: "replay-1",
		Payload: &webapiv1.Frame_ReplayRequest{ReplayRequest: &webapiv1.ReplayRequest{
			LastSeq: 60,
		}},
	})

	type replayedFrame struct {
		seq  int64
		kind string
	}
	replayed := make([]replayedFrame, 0, 4)
	for {
		frame := readFrameWithin(t, alice, time.Second)
		switch {
		case frame.GetStructureChanged() != nil:
			changed := frame.GetStructureChanged()
			replayed = append(replayed, replayedFrame{seq: changed.GetSeq(), kind: changed.GetChangeType()})
		case frame.GetCellChanged() != nil:
			replayed = append(replayed, replayedFrame{seq: frame.GetCellChanged().GetSeq()})
		case frame.GetAck().GetMsgId() == "replay-1":
			want := []replayedFrame{
				{61, "field_created"},
				{62, "field_created"},
				{62, ""},
				{63, "table_renamed"},
			}
			if len(replayed) != len(want) {
				t.Fatalf("replayed = %+v, want %v", replayed, want)
			}
			for index, item := range want {
				if replayed[index] != item {
					t.Fatalf("replayed[%d] = %+v, want %+v; all = %+v", index, replayed[index], item, replayed)
				}
			}
			return
		}
	}
}
