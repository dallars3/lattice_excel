// realtime/events_rpc.go：断线重放的生产适配——core records.GetEventsSince
// 持久事件流转换为 WebSocket 补发帧（realtime.EventStore seam）。
package realtime

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	sharev1 "excel/api/share/v1"
	webapiv1 "excel/api/webapi/v1"
)

// ReplayEventsRPC is the records streaming seam for durable replay.
type ReplayEventsRPC interface {
	GetEventsSince(
		ctx context.Context,
		request *recordsv1.GetEventsSinceRequest,
	) (recordsv1.RecordsService_GetEventsSinceClient, error)
}

// RPCEventStore replays missed increments from the core outbox event stream.
// 补发帧与在线广播（subscribers.go）共用同一组装逻辑，保证断线前后帧形状一致。
type RPCEventStore struct {
	records ReplayEventsRPC
}

var _ EventStore = (*RPCEventStore)(nil)

// NewRPCEventStore creates the production replay event store.
func NewRPCEventStore(records ReplayEventsRPC) *RPCEventStore {
	return &RPCEventStore{records: records}
}

// Since converts durable events after lastSeq into replay frames. 非增量事件
// （通知/吊销/审计等）不产生帧；流式读取失败原样上抛，由 hub 关闭本次补发。
func (store *RPCEventStore) Since(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID, tableID string,
	lastSeq int64,
	limit int,
) ([]*webapiv1.Frame, error) {
	if store == nil || store.records == nil {
		return nil, errors.New("records event stream is required")
	}
	outgoing, err := replayCallContext(ctx, principal)
	if err != nil {
		return nil, err
	}
	stream, err := store.records.GetEventsSince(outgoing, &recordsv1.GetEventsSinceRequest{
		BaseId:  baseID,
		TableId: tableID,
		LastSeq: lastSeq,
		Limit:   int32(limit),
	})
	if err != nil {
		return nil, err
	}
	frames := make([]*webapiv1.Frame, 0)
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return frames, nil
		}
		if err != nil {
			return nil, err
		}
		frames = append(frames, syncEventFrames(response.GetEvent())...)
	}
}

// replayCallContext 把调用者身份折进补发调用（x-principal 与网关同一 base64
// proto 传输约定）；行级范围沿用 records 会话路由口径（filler→OWN，其余 ALL），
// GetEventsSince 只做校验、不按范围过滤，房间语义由握手裁决承担。
func replayCallContext(ctx context.Context, principal *commonv1.Principal) (context.Context, error) {
	if principal == nil || principal.GetUserId() == "" {
		return nil, errors.New("replay principal is required")
	}
	payload, err := proto.Marshal(principal)
	if err != nil {
		return nil, errors.New("replay principal marshal failed")
	}
	return metadata.AppendToOutgoingContext(
		ctx,
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
		"x-row-scope",
		replayRowScope(principal),
	), nil
}

func replayRowScope(principal *commonv1.Principal) string {
	role := strings.TrimPrefix(
		strings.ToLower(strings.TrimSpace(principal.GetLinkContext().GetRole())),
		"role_",
	)
	if role == "filler" {
		return sharev1.RowScope_ROW_SCOPE_OWN.String()
	}
	return sharev1.RowScope_ROW_SCOPE_ALL.String()
}

// syncEventFrames 把一条持久事件转换为补发帧序列：record.changed（结构/单元格）、
// record.deleted（按记录展开）；其余事件类型不是增量帧，返回空。单个事件载荷
// 解码/组帧失败只记一条 Warn 后跳过（畸形事件不得中断整次补发），不静默吞错。
func syncEventFrames(event *recordsv1.SyncEvent) []*webapiv1.Frame {
	if event == nil || event.GetHeader() == nil {
		return nil
	}
	payload, err := syncEventPayload(event)
	if err != nil {
		slog.Warn("sync event payload decode failed; replay event skipped",
			"event_id", event.GetHeader().GetEventId(), "type", event.GetHeader().GetType(), "error", err)
		return nil
	}
	seq := event.GetHeader().GetSeq()
	switch event.GetHeader().GetType() {
	case "record.changed":
		if isStructureChange(payload.ChangeType) {
			return []*webapiv1.Frame{structureChangedEventFrame(seq, payload)}
		}
		frame, err := cellChangedEventFrame(seq, payload)
		if err != nil {
			slog.Warn("cell changed frame build failed; replay event skipped",
				"event_id", event.GetHeader().GetEventId(), "type", event.GetHeader().GetType(), "error", err)
			return nil
		}
		return []*webapiv1.Frame{frame}
	case "record.deleted":
		return recordDeletedEventFrames(seq, payload)
	default:
		return nil
	}
}

func syncEventPayload(event *recordsv1.SyncEvent) (eventPayload, error) {
	payloadJSON, err := protojson.Marshal(event.GetPayloadMap())
	if err != nil {
		return eventPayload{}, err
	}
	return decodeEventPayload(payloadJSON)
}
