// realtime/framecodec.go：WebSocket 的 protojson 编解码。
package realtime

import (
	"bytes"
	"encoding/json"

	"google.golang.org/protobuf/encoding/protojson"

	webapiv1 "excel/api/webapi/v1"
)

var frameMarshalOptions = protojson.MarshalOptions{UseProtoNames: true}

// frameSeqPayloadFields 列出必须在线上显式携带 seq 的帧（含 seq=0）。
var frameSeqPayloadFields = []string{
	"snapshot",
	"cell_changed",
	"record_deleted",
	"structure_changed",
}

func marshalFrame(frame *webapiv1.Frame) ([]byte, error) {
	payload, err := frameMarshalOptions.Marshal(frame)
	if err != nil {
		return nil, err
	}
	return ensureFrameShape(payload)
}

func unmarshalFrame(payload []byte) (*webapiv1.Frame, error) {
	var frame webapiv1.Frame
	if err := protojson.Unmarshal(payload, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

// ensureFrameShape 补齐 protojson 默认省略的零值：约定的帧类型必须显式输出 seq
// （含 0），snapshot 的空 member_list 必须输出 []。只按字段补零，不开启
// EmitUnpopulated，避免把未设置的 message 字段输出成 null 污染所有帧。
func ensureFrameShape(payload []byte) ([]byte, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(payload, &document); err != nil {
		return nil, err
	}
	modified := false
	for _, name := range frameSeqPayloadFields {
		raw, ok := document[name]
		if !ok {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		changed := false
		if _, ok := object["seq"]; !ok {
			object["seq"] = json.RawMessage(`"0"`)
			changed = true
		}
		if name == "snapshot" {
			if _, ok := object["member_list"]; !ok {
				object["member_list"] = json.RawMessage(`[]`)
				changed = true
			}
		}
		if !changed {
			continue
		}
		encoded, err := marshalJSON(object)
		if err != nil {
			return nil, err
		}
		document[name] = encoded
		modified = true
	}
	if !modified {
		return payload, nil
	}
	return marshalJSON(document)
}

func marshalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
