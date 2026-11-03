package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/types/known/emptypb"

	notifyv1 "excel/api/notify/v1"
	"excel_web_api/deal"
	"excel_web_api/rpc"
)

type notifyGatewayStub struct {
	listCalls  int
	countCalls int
	readCalls  int
	allCalls   int
	lastList   *notifyv1.ListNotificationsRequest
	lastRead   *notifyv1.MarkReadRequest
}

func (stub *notifyGatewayStub) ListNotifications(
	_ context.Context,
	request *notifyv1.ListNotificationsRequest,
) (*notifyv1.ListNotificationsResponse, error) {
	stub.listCalls++
	stub.lastList = request
	return &notifyv1.ListNotificationsResponse{
		NotificationList: []*notifyv1.Notification{{
			Id:        "notification-1",
			Type:      "comment.mentioned",
			BaseId:    "base-1",
			RecordId:  "record-1",
			CommentId: "comment-1",
		}},
		HasMore:    true,
		NextCursor: "next",
	}, nil
}

func (stub *notifyGatewayStub) GetUnreadCount(
	context.Context,
	*emptypb.Empty,
) (*notifyv1.GetUnreadCountResponse, error) {
	stub.countCalls++
	return &notifyv1.GetUnreadCountResponse{Count: 3}, nil
}

func (stub *notifyGatewayStub) MarkRead(
	_ context.Context,
	request *notifyv1.MarkReadRequest,
) (*emptypb.Empty, error) {
	stub.readCalls++
	stub.lastRead = request
	return &emptypb.Empty{}, nil
}

func (stub *notifyGatewayStub) MarkAllRead(
	context.Context,
	*emptypb.Empty,
) (*emptypb.Empty, error) {
	stub.allCalls++
	return &emptypb.Empty{}, nil
}

var _ rpc.NotifyRPC = (*notifyGatewayStub)(nil)

func TestNotifyRoutesRequireSessionAndForwardOneRPCPerRequest(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	client := &notifyGatewayStub{}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterNotifyRoutes(pipeline, client)

	list := performNotifyRequest(t, pipeline, http.MethodGet, "/api/v1/notifications?cursor=page&limit=2")
	assertNotifySuccess(t, list, func(data map[string]any) {
		notificationList, ok := data["notification_list"].([]any)
		if !ok || len(notificationList) != 1 || data["has_more"] != true {
			t.Fatalf("list data = %+v", data)
		}
	})
	if client.listCalls != 1 ||
		client.lastList.GetCursor() != "page" ||
		client.lastList.GetLimit() != 2 {
		t.Fatalf("ListNotifications calls=%d request=%v", client.listCalls, client.lastList)
	}

	unread := performNotifyRequest(t, pipeline, http.MethodGet, "/api/v1/notifications/unread-count")
	assertNotifySuccess(t, unread, func(data map[string]any) {
		if data["count"] != float64(3) {
			t.Fatalf("unread data = %+v", data)
		}
	})
	if client.countCalls != 1 {
		t.Fatalf("GetUnreadCount calls = %d, want 1", client.countCalls)
	}

	read := performNotifyRequest(t, pipeline, http.MethodPost, "/api/v1/notifications/notification-1/read")
	assertNotifySuccess(t, read, func(map[string]any) {})
	if client.readCalls != 1 || client.lastRead.GetNotificationId() != "notification-1" {
		t.Fatalf("MarkRead calls=%d request=%v", client.readCalls, client.lastRead)
	}

	all := performNotifyRequest(t, pipeline, http.MethodPost, "/api/v1/notifications/read-all")
	assertNotifySuccess(t, all, func(map[string]any) {})
	if client.allCalls != 1 {
		t.Fatalf("MarkAllRead calls = %d, want 1", client.allCalls)
	}

	missingSession := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	missingResponse := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(missingResponse, missingSession)
	if missingResponse.Code != http.StatusUnauthorized {
		t.Fatalf("notifications without session = HTTP %d, want 401", missingResponse.Code)
	}
}

func TestNotifyRoutesRejectMalformedLimitWithoutRPC(t *testing.T) {
	iam := &pipelineIamRPC{exchangeResponse: allowedSession("user-1", "enterprise-1")}
	client := &notifyGatewayStub{}
	pipeline := NewPipeline(iam, nil, nil, nil)
	RegisterNotifyRoutes(pipeline, client)

	response := performNotifyRequest(t, pipeline, http.MethodGet, "/api/v1/notifications?limit=bad")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != deal.CodeInvalidArgument || client.listCalls != 0 {
		t.Fatalf("code=%d listCalls=%d, want 1001/0", envelope.Code, client.listCalls)
	}
}

func performNotifyRequest(
	t *testing.T,
	pipeline *Pipeline,
	method, path string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()
	pipeline.Handler().ServeHTTP(response, request)
	return response
}

func assertNotifySuccess(
	t *testing.T,
	response *httptest.ResponseRecorder,
	assert func(map[string]any),
) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("envelope = %+v, want success", envelope)
	}
	assert(envelope.Data)
}
