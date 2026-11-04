// gateway/tracking_routes.go：对客埋点批上报端点（会话鉴权、逐条校验、Redis 短期幂等、
// 服务端盖章后经事件总线发布 track.reported；端点自身不产生埋点）。
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"excel-libs/eventbus"
	"excel-libs/idgen"

	commonv1 "excel/api/common/v1"
	"excel_web_api/deal"
)

const (
	// maxTrackingBatchSize 单批事件条数上限：与 records 分页 limit 上限（100）一致，
	// 一次提交最多 100 条，避免单请求放大为无界批量。
	maxTrackingBatchSize = 100

	// trackingEventIDMinLength / trackingEventIDMaxLength 约束客户端事件幂等键：
	// 前端 tracker 使用 ULID（26 字符），下限 8 挡住空串/过短脏值，上限 64 约束 Redis 键与 core 落库列。
	trackingEventIDMinLength = 8
	trackingEventIDMaxLength = 64

	// trackingEventNameMaxLength 约束 event_name 长度上限（附录 C 事件名均 < 64）。
	trackingEventNameMaxLength = 64

	// maxTrackingDetailBytes 单条 detail 序列化后字节上限 4KB，
	// 约束事件体积与 core 落库列，防埋点洪峰撑大存储。
	maxTrackingDetailBytes = 4096

	// trackingDedupeTTL Redis 短期幂等去重窗口（24h）。
	trackingDedupeTTL = 24 * time.Hour
)

// trackingDeduper 埋点事件短期幂等去重接口（Redis 实现，故障放行）。
type trackingDeduper interface {
	Dedupe(context.Context, string, time.Duration) (bool, error)
}

var trackingEventNamePattern = regexp.MustCompile(`^fe_[a-z0-9_]+$`)

type trackingRoutes struct {
	bus    eventbus.EventBus
	dedupe trackingDeduper
	logger *slog.Logger
}

// RegisterTrackingRoutes binds the logged-in tracking batch endpoint behind
// SessionRequired（匿名由管线统一返回 2001）。
func RegisterTrackingRoutes(
	pipeline *Pipeline,
	bus eventbus.EventBus,
	dedupe trackingDeduper,
) {
	if pipeline == nil {
		panic("tracking routes pipeline is required")
	}
	routes := &trackingRoutes{bus: bus, dedupe: dedupe, logger: slog.Default()}
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/tracking/batch",
		http.HandlerFunc(routes.batchIngest),
	)
}

type trackingBatchRequest struct {
	Events []trackingEventRequest `json:"events"`
}

type trackingEventRequest struct {
	EventID    string          `json:"event_id"`
	EventName  string          `json:"event_name"`
	Detail     json.RawMessage `json:"detail"`
	ClientAtMs int64           `json:"client_at_ms"`
}

type trackingBatchResult struct {
	Accepted   int `json:"accepted"`
	Duplicated int `json:"duplicated"`
	Rejected   int `json:"rejected"`
}

type trackingReportedPayload struct {
	EventID      string          `json:"event_id"`
	EventName    string          `json:"event_name"`
	Detail       json.RawMessage `json:"detail"`
	ClientAtMs   int64           `json:"client_at_ms,omitempty"`
	UserID       string          `json:"user_id"`
	UserType     string          `json:"user_type"`
	ReceivedAtMs int64           `json:"received_at_ms"`
}

func (routes *trackingRoutes) batchIngest(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload trackingBatchRequest
	if err := decodeJSONBody(response, request, &payload); err != nil ||
		len(payload.Events) == 0 || len(payload.Events) > maxTrackingBatchSize {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	principal, ok := PrincipalFromContext(request.Context())
	if !ok {
		writeCodeError(response, deal.CodeUnauthenticated)
		return
	}

	result := trackingBatchResult{}
	userType := trackingUserType(principal)
	userID := principal.GetUserId()

	for _, event := range payload.Events {
		detail, valid := normalizeTrackingDetail(event.Detail)
		if !valid || !validTrackingEvent(event.EventID, event.EventName, len(detail)) {
			result.Rejected++
			continue
		}
		if !routes.claimEvent(request.Context(), event.EventID) {
			result.Duplicated++
			continue
		}
		receivedAtMs := time.Now().UTC().UnixMilli()
		if err := routes.publishTracked(
			request.Context(),
			event,
			detail,
			receivedAtMs,
			userID,
			userType,
		); err != nil {
			// 发布失败 503 且不重试放大；前端静默丢弃本批。
			routes.logger.Error("publish track.reported failed",
				"error", err, "event_id", event.EventID)
			writeCodeError(response, deal.CodeServiceUnavailable)
			return
		}
		result.Accepted++
	}
	writeSuccess(response, result)
}

func (routes *trackingRoutes) claimEvent(ctx context.Context, eventID string) bool {
	if routes.dedupe == nil {
		return true
	}
	first, err := routes.dedupe.Dedupe(ctx, eventID, trackingDedupeTTL)
	if err != nil {
		// 故障放行：去重依赖不可用时视作首次受理，避免误伤追踪（与限流 fail-open 同策略）。
		routes.logger.Error("tracking dedupe unavailable; event continues (fail-open)",
			"error", err, "event_id", eventID)
		return true
	}
	return first
}

func (routes *trackingRoutes) publishTracked(
	ctx context.Context,
	event trackingEventRequest,
	detail json.RawMessage,
	receivedAtMs int64,
	userID string,
	userType string,
) error {
	if routes.bus == nil {
		return errors.New("tracking event bus is required")
	}
	payload, err := json.Marshal(trackingReportedPayload{
		EventID:      event.EventID,
		EventName:    event.EventName,
		Detail:       detail,
		ClientAtMs:   event.ClientAtMs,
		UserID:       userID,
		UserType:     userType,
		ReceivedAtMs: receivedAtMs,
	})
	if err != nil {
		return err
	}
	envelopeID, err := idgen.New()
	if err != nil {
		return err
	}
	return routes.bus.Publish(ctx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    envelopeID,
			Type:       "track.reported",
			Version:    1,
			OccurredAt: receivedAtMs,
			Producer:   "tracking",
		},
		Payload: payload,
	})
}

func validTrackingEvent(eventID string, eventName string, detailBytes int) bool {
	if len(eventID) < trackingEventIDMinLength || len(eventID) > trackingEventIDMaxLength {
		return false
	}
	if len(eventName) > trackingEventNameMaxLength || !trackingEventNamePattern.MatchString(eventName) {
		return false
	}
	if detailBytes <= 0 || detailBytes > maxTrackingDetailBytes {
		return false
	}
	return true
}

// normalizeTrackingDetail 校验 detail 为 JSON 对象并返回规范化（紧凑、键有序）
// 序列化结果；任一不满足（空/非对象/超 4KB）返回 ok=false。
func normalizeTrackingDetail(detail json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(detail)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var object any
	if err := decoder.Decode(&object); err != nil {
		return nil, false
	}
	if _, ok := object.(map[string]any); !ok {
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false
	}
	canonical, err := json.Marshal(object)
	if err != nil || len(canonical) > maxTrackingDetailBytes {
		return nil, false
	}
	return canonical, true
}

func trackingUserType(principal *commonv1.Principal) string {
	if principal.GetInternal() {
		return "internal"
	}
	return "external"
}