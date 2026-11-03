// rpc/notify.go：该模块 gRPC client 封装（文件名=模块名）。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	notifyv1 "excel/api/notify/v1"
)

// NotifyRPC is the notification surface consumed by the HTTP gateway.
type NotifyRPC interface {
	ListNotifications(
		context.Context,
		*notifyv1.ListNotificationsRequest,
	) (*notifyv1.ListNotificationsResponse, error)
	GetUnreadCount(
		context.Context,
		*emptypb.Empty,
	) (*notifyv1.GetUnreadCountResponse, error)
	MarkRead(
		context.Context,
		*notifyv1.MarkReadRequest,
	) (*emptypb.Empty, error)
	MarkAllRead(
		context.Context,
		*emptypb.Empty,
	) (*emptypb.Empty, error)
}

// NotifyClient adapts the generated worker gRPC client to NotifyRPC.
type NotifyClient struct {
	client notifyv1.NotifyServiceClient
}

var _ NotifyRPC = (*NotifyClient)(nil)

// NewNotifyClient wraps a worker-service gRPC connection.
func NewNotifyClient(connection grpc.ClientConnInterface) *NotifyClient {
	return &NotifyClient{client: notifyv1.NewNotifyServiceClient(connection)}
}

// ListNotifications forwards one notification page request.
func (client *NotifyClient) ListNotifications(
	ctx context.Context,
	request *notifyv1.ListNotificationsRequest,
) (*notifyv1.ListNotificationsResponse, error) {
	return client.client.ListNotifications(ctx, request)
}

// GetUnreadCount forwards one unread-count lookup.
func (client *NotifyClient) GetUnreadCount(
	ctx context.Context,
	request *emptypb.Empty,
) (*notifyv1.GetUnreadCountResponse, error) {
	return client.client.GetUnreadCount(ctx, request)
}

// MarkRead forwards one idempotent single-notification read.
func (client *NotifyClient) MarkRead(
	ctx context.Context,
	request *notifyv1.MarkReadRequest,
) (*emptypb.Empty, error) {
	return client.client.MarkRead(ctx, request)
}

// MarkAllRead forwards one idempotent mark-all-read request.
func (client *NotifyClient) MarkAllRead(
	ctx context.Context,
	request *emptypb.Empty,
) (*emptypb.Empty, error) {
	return client.client.MarkAllRead(ctx, request)
}
