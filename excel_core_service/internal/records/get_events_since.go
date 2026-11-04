// records/get_events_since.go：EventBus 有序增量回放。
package records

import (
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	"excel_core_service/deal"
)

const (
	defaultEventLimit = 100
	maxEventLimit     = 500
)

// GetEventsSince streams durable events after the caller's last sequence.
func (service *Service) GetEventsSince(
	request *recordsv1.GetEventsSinceRequest,
	stream recordsv1.RecordsService_GetEventsSinceServer,
) error {
	if service == nil || service.repository == nil {
		return errors.New("records repository is not configured")
	}
	if request == nil || strings.TrimSpace(request.GetTableId()) == "" {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	ctx := stream.Context()
	if _, err := deal.PrincipalFromContext(ctx); err != nil {
		return err
	}
	if _, err := rowScopeFromContext(ctx); err != nil {
		return err
	}
	events, err := service.repository.OutboxEventsSince(
		ctx,
		request.GetBaseId(),
		request.GetTableId(),
		request.GetLastSeq(),
		normalizeEventLimit(request.GetLimit()),
	)
	if err != nil {
		return err
	}
	for _, event := range events {
		payload, err := decodeEventPayload(event.Payload)
		if err != nil {
			return status.Errorf(
				codes.Internal,
				"decode outbox event %s payload: %v",
				event.EventID,
				err,
			)
		}
		if err := stream.Send(&recordsv1.GetEventsSinceResponse{
			Event: &recordsv1.SyncEvent{
				Header: &commonv1.EventHeader{
					EventId:    event.EventID,
					Type:       event.Type,
					Version:    event.Version,
					BaseId:     event.BaseID,
					Seq:        event.EffectiveSeq,
					OccurredAt: event.OccurredAt,
					Producer:   event.Producer,
				},
				PayloadMap: payload,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func normalizeEventLimit(limit int32) int {
	if limit <= 0 {
		return defaultEventLimit
	}
	if limit > maxEventLimit {
		return maxEventLimit
	}
	return int(limit)
}

func decodeEventPayload(payload []byte) (*structpb.Struct, error) {
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	return structpb.NewStruct(decoded)
}
