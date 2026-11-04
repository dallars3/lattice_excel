// notify/common.go：通知共享身份、投影、事件与保留期规则。
package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"excel-libs/eventbus"
	"excel-libs/idgen"
	notifyv1 "excel/api/notify/v1"
	"excel_worker_service/base"
	"excel_worker_service/deal"
	workerredis "excel_worker_service/redis"
)

const (
	// NotifyRetention is the user-visible and query-side notification retention.
	NotifyRetention    = 90 * 24 * time.Hour
	defaultNotifyLimit = 50
	maxNotifyLimit     = 100
	notifyConsumerName = "worker-notify"
)

var notifyTopics = []string{
	"comment.created",
	"comment.mentioned",
	"invite.created",
	"invite.canceled",
	"member.disabled",
	"member.enabled",
	"enterprise.owner_transferred",
	"link.closed",
	"share.changed",
	"import.finished",
}

// Service implements notify RPCs and consumes collaboration events.
type Service struct {
	notifyv1.UnimplementedNotifyServiceServer

	repository *base.NotifyRepository
	counters   *workerredis.NotifyCounters
	bus        eventbus.EventBus
	now        func() time.Time
	logger     *slog.Logger
}

// NewService constructs the notify service. The optional logger defaults to slog.Default.
func NewService(
	repository *base.NotifyRepository,
	counters *workerredis.NotifyCounters,
	bus eventbus.EventBus,
	now func() time.Time,
	loggers ...*slog.Logger,
) *Service {
	if now == nil {
		now = time.Now
	}
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &Service{
		repository: repository,
		counters:   counters,
		bus:        bus,
		now:        now,
		logger:     logger,
	}
}

// Register installs the notify service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *Service) {
	notifyv1.RegisterNotifyServiceServer(registrar, service)
}


func notificationToProto(notification base.Notification) (*notifyv1.Notification, error) {
	payload := &structpb.Struct{}
	if len(notification.PayloadMap) > 0 {
		var payloadMap map[string]any
		if err := json.Unmarshal(notification.PayloadMap, &payloadMap); err != nil {
			return nil, fmt.Errorf("decode notification payload: %w", err)
		}
		converted, err := structpb.NewStruct(payloadMap)
		if err != nil {
			return nil, fmt.Errorf("convert notification payload: %w", err)
		}
		payload = converted
	}
	return &notifyv1.Notification{
		Id:         notification.ID,
		Type:       notification.Type,
		BaseId:     notification.BaseID,
		RecordId:   notification.RecordID,
		CommentId:  notification.CommentID,
		PayloadMap: payload,
		Read:       notification.Read,
		CreatedAt:  notification.CreatedAt,
	}, nil
}

func normalizeLimit(limit int32) (int, error) {
	if limit < 0 {
		return 0, deal.RPCError(deal.CodeInvalidArgument)
	}
	if limit == 0 {
		return defaultNotifyLimit, nil
	}
	if limit > maxNotifyLimit {
		return maxNotifyLimit, nil
	}
	return int(limit), nil
}

type cursorPayload struct {
	CreatedAt int64  `json:"created_at"`
	ID        string `json:"id"`
}

func encodeCursor(cursor base.NotificationCursor) string {
	payload, _ := json.Marshal(cursorPayload{CreatedAt: cursor.CreatedAt, ID: cursor.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(value string) (*base.NotificationCursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	var payload cursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil ||
		payload.CreatedAt <= 0 || payload.ID == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	return &base.NotificationCursor{CreatedAt: payload.CreatedAt, ID: payload.ID}, nil
}

func (service *Service) retentionCutoff() int64 {
	return service.now().UTC().Add(-NotifyRetention).UnixMilli()
}

func (service *Service) publishCreated(
	ctx context.Context,
	notification base.Notification,
) error {
	if service.bus == nil {
		return errors.New("notify event bus is required")
	}
	payload, err := json.Marshal(map[string]any{
		"notification_id": notification.ID,
		"user_id":         notification.UserID,
		"type":            notification.Type,
		"base_id":         notification.BaseID,
	})
	if err != nil {
		return fmt.Errorf("marshal notify.created payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate notify.created event id: %w", err)
	}
	return service.bus.Publish(ctx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "notify.created",
			Version:    1,
			BaseID:     notification.BaseID,
			OccurredAt: service.now().UTC().UnixMilli(),
			Producer:   "notify",
		},
		Payload: payload,
	})
}

