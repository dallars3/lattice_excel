// gateway/notify_routes.go：通知中心 HTTP 路由（H-73 ~ H-76）。
package gateway

import (
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	notifyv1 "excel/api/notify/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type notifyRoutes struct {
	client rpc.NotifyRPC
}

// RegisterNotifyRoutes binds H-73 through H-76 behind SessionRequired.
func RegisterNotifyRoutes(pipeline *Pipeline, client rpc.NotifyRPC) {
	if pipeline == nil {
		panic("notify routes pipeline is required")
	}
	routes := &notifyRoutes{client: client}
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/notifications",
		http.HandlerFunc(routes.listNotifications),
	)
	pipeline.SessionRequired(
		http.MethodGet,
		"/api/v1/notifications/unread-count",
		http.HandlerFunc(routes.getUnreadCount),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/notifications/{notification_id}/read",
		http.HandlerFunc(routes.markRead),
	)
	pipeline.SessionRequired(
		http.MethodPost,
		"/api/v1/notifications/read-all",
		http.HandlerFunc(routes.markAllRead),
	)
}

func (routes *notifyRoutes) listNotifications(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	limit, ok := notifyQueryLimit(request)
	if !ok {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.ListNotifications(
		request.Context(),
		&notifyv1.ListNotificationsRequest{
			Cursor: request.URL.Query().Get("cursor"),
			Limit:  limit,
		},
	)
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeNotifyResult(response, result)
}

func (routes *notifyRoutes) getUnreadCount(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.GetUnreadCount(request.Context(), &emptypb.Empty{})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeNotifyResult(response, result)
}

func (routes *notifyRoutes) markRead(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	notificationID := strings.TrimSpace(request.PathValue("notification_id"))
	if notificationID == "" {
		writeCodeError(response, deal.CodeInvalidArgument)
		return
	}
	result, err := routes.client.MarkRead(request.Context(), &notifyv1.MarkReadRequest{
		NotificationId: notificationID,
	})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeNotifyResult(response, result)
}

func (routes *notifyRoutes) markAllRead(
	response http.ResponseWriter,
	request *http.Request,
) {
	if routes.client == nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	result, err := routes.client.MarkAllRead(request.Context(), &emptypb.Empty{})
	if err != nil {
		writeBusinessError(response, err)
		return
	}
	writeNotifyResult(response, result)
}

func notifyQueryLimit(request *http.Request) (int32, bool) {
	raw := strings.TrimSpace(request.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value <= 0 {
		return 0, false
	}
	if value > 100 {
		value = 100
	}
	return int32(value), true
}

func writeNotifyResult(response http.ResponseWriter, message proto.Message) {
	payload, err := protoJSON(message)
	if err != nil {
		writeCodeError(response, deal.CodeServiceUnavailable)
		return
	}
	writeSuccess(response, payload)
}
