// rpc/share.go：share gRPC client 的网关鉴权子集。
package rpc

import (
	"context"

	"google.golang.org/grpc"

	sharev1 "excel/api/share/v1"
)

// ShareRPC is the share subset consumed by the gateway authorization pipeline.
type ShareRPC interface {
	CheckAccess(context.Context, *sharev1.CheckAccessRequest) (*sharev1.CheckAccessResponse, error)
	ResolveRole(context.Context, *sharev1.ResolveRoleRequest) (*sharev1.ResolveRoleResponse, error)
}

// ShareClient adapts the generated ShareService client to ShareRPC.
type ShareClient struct {
	client sharev1.ShareServiceClient
}

var _ ShareRPC = (*ShareClient)(nil)

// NewShareClient wraps a core-service gRPC connection.
func NewShareClient(connection grpc.ClientConnInterface) *ShareClient {
	return &ShareClient{client: sharev1.NewShareServiceClient(connection)}
}

// CheckAccess asks core.share whether a principal may perform an action.
func (client *ShareClient) CheckAccess(
	ctx context.Context,
	request *sharev1.CheckAccessRequest,
) (*sharev1.CheckAccessResponse, error) {
	return client.client.CheckAccess(ctx, request)
}

// ResolveRole resolves the effective Base role without checking an action.
func (client *ShareClient) ResolveRole(
	ctx context.Context,
	request *sharev1.ResolveRoleRequest,
) (*sharev1.ResolveRoleResponse, error) {
	return client.client.ResolveRole(ctx, request)
}
