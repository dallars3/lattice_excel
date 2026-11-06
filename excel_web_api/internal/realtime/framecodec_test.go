package realtime

import (
	"encoding/json"
	"testing"

	webapiv1 "excel/api/webapi/v1"
)

// TestMarshalFrameEmitsZeroSeqAndEmptyListFields 锁实时帧形状：seq 为 0 时也必须出现，
// snapshot 的空 member_list 必须是 []；同时不能靠 EmitUnpopulated 把未设置的
// message 字段输出成 null。
func TestMarshalFrameEmitsZeroSeqAndEmptyListFields(t *testing.T) {
	frames := map[string]*webapiv1.Frame{
		"snapshot": {Payload: &webapiv1.Frame_Snapshot{
			Snapshot: &webapiv1.Snapshot{},
		}},
		"cell_changed": {Payload: &webapiv1.Frame_CellChanged{
			CellChanged: &webapiv1.CellChanged{},
		}},
		"record_deleted": {Payload: &webapiv1.Frame_RecordDeleted{
			RecordDeleted: &webapiv1.RecordDeleted{},
		}},
		"structure_changed": {Payload: &webapiv1.Frame_StructureChanged{
			StructureChanged: &webapiv1.StructureChanged{},
		}},
	}
	for name, frame := range frames {
		payload, err := marshalFrame(frame)
		if err != nil {
			t.Fatalf("%s marshalFrame() error = %v", name, err)
		}
		var document map[string]json.RawMessage
		if err := json.Unmarshal(payload, &document); err != nil {
			t.Fatalf("%s frame is not a JSON object: %s", name, payload)
		}
		raw, ok := document[name]
		if !ok {
			t.Fatalf("%s payload missing from %s", name, payload)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatalf("%s payload is not an object: %s", name, raw)
		}
		seq, ok := object["seq"]
		if !ok {
			t.Fatalf("%s frame omitted zero seq: %s", name, payload)
		}
		if sequence := string(seq); sequence != `"0"` && sequence != `0` {
			t.Fatalf("%s seq = %s, want 0", name, sequence)
		}
		if name == "snapshot" {
			members, ok := object["member_list"]
			if !ok {
				t.Fatalf("snapshot omitted empty member_list: %s", payload)
			}
			if string(members) != "[]" {
				t.Fatalf("snapshot member_list = %s, want []", members)
			}
		}
	}
}

// TestMarshalFrameKeepsUnsetSubmessagesOut 防止 EmitUnpopulated 污染所有帧：
// 仅含 snapshot 的帧不得出现其它 payload 的 null 占位。
func TestMarshalFrameKeepsUnsetSubmessagesOut(t *testing.T) {
	payload, err := marshalFrame(&webapiv1.Frame{
		MsgId:   "m1",
		Payload: &webapiv1.Frame_Ack{Ack: &webapiv1.Ack{MsgId: "m1"}},
	})
	if err != nil {
		t.Fatalf("marshalFrame() error = %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("frame is not a JSON object: %s", payload)
	}
	for _, absent := range []string{
		"snapshot", "cell_changed", "record_deleted", "structure_changed",
	} {
		if _, ok := document[absent]; ok {
			t.Fatalf("ack frame contains unset %s: %s", absent, payload)
		}
	}
}
