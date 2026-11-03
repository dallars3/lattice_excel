package rpc_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	notifyv1 "excel/api/notify/v1"
	"excel_web_api/rpc"
)

type notifyRPCTestServer struct {
	notifyv1.UnimplementedNotifyServiceServer
	listRequest *notifyv1.ListNotificationsRequest
	readRequest *notifyv1.MarkReadRequest
}

func (server *notifyRPCTestServer) ListNotifications(
	_ context.Context,
	request *notifyv1.ListNotificationsRequest,
) (*notifyv1.ListNotificationsResponse, error) {
	server.listRequest = request
	return &notifyv1.ListNotificationsResponse{
		NotificationList: []*notifyv1.Notification{{Id: "notification-1"}},
		HasMore:          true,
		NextCursor:       "next",
	}, nil
}

func (server *notifyRPCTestServer) GetUnreadCount(
	context.Context,
	*emptypb.Empty,
) (*notifyv1.GetUnreadCountResponse, error) {
	return &notifyv1.GetUnreadCountResponse{Count: 7}, nil
}

func (server *notifyRPCTestServer) MarkRead(
	_ context.Context,
	request *notifyv1.MarkReadRequest,
) (*emptypb.Empty, error) {
	server.readRequest = request
	return &emptypb.Empty{}, nil
}

func (server *notifyRPCTestServer) MarkAllRead(
	context.Context,
	*emptypb.Empty,
) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func TestNotifyClientForwardsFourRPCs(t *testing.T) {
	serverImpl := &notifyRPCTestServer{}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	notifyv1.RegisterNotifyServiceServer(server, serverImpl)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(
		"passthrough:///notify-rpc-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := rpc.NewNotifyClient(connection)
	ctx := context.Background()

	list, err := client.ListNotifications(ctx, &notifyv1.ListNotificationsRequest{
		Cursor: "cursor",
		Limit:  20,
	})
	if err != nil || len(list.GetNotificationList()) != 1 ||
		serverImpl.listRequest.GetCursor() != "cursor" ||
		serverImpl.listRequest.GetLimit() != 20 {
		t.Fatalf("ListNotifications() = %v, %v request=%v", list, err, serverImpl.listRequest)
	}
	count, err := client.GetUnreadCount(ctx, &emptypb.Empty{})
	if err != nil || count.GetCount() != 7 {
		t.Fatalf("GetUnreadCount() = %v, %v", count, err)
	}
	if _, err := client.MarkRead(ctx, &notifyv1.MarkReadRequest{
		NotificationId: "notification-1",
	}); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	if serverImpl.readRequest.GetNotificationId() != "notification-1" {
		t.Fatalf("MarkRead request = %v", serverImpl.readRequest)
	}
	if _, err := client.MarkAllRead(ctx, &emptypb.Empty{}); err != nil {
		t.Fatalf("MarkAllRead() error = %v", err)
	}
}
