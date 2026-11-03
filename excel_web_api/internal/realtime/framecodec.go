// realtime/framecodec.go：WebSocket 的 protojson 编解码。
package realtime

import (
	"google.golang.org/protobuf/encoding/protojson"

	webapiv1 "excel/api/webapi/v1"
)

var frameMarshalOptions = protojson.MarshalOptions{UseProtoNames: true}

func marshalFrame(frame *webapiv1.Frame) ([]byte, error) {
	return frameMarshalOptions.Marshal(frame)
}

func unmarshalFrame(payload []byte) (*webapiv1.Frame, error) {
	var frame webapiv1.Frame
	if err := protojson.Unmarshal(payload, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}
