// rpc/workspace.go：core.workspace 客户端。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	workspacev1 "excel/api/workspace/v1"
)

// WorkspaceRPC is the core workspace surface used by import/export naming.
type WorkspaceRPC interface {
	GetHome(
		context.Context,
		*emptypb.Empty,
	) (*workspacev1.GetHomeResponse, error)
	CreateBase(
		context.Context,
		*workspacev1.CreateBaseRequest,
	) (*workspacev1.CreateBaseResponse, error)
}

// WorkspaceClient adapts the generated core workspace client.
type WorkspaceClient struct {
	client workspacev1.WorkspaceServiceClient
}

var _ WorkspaceRPC = (*WorkspaceClient)(nil)

// NewWorkspaceClient wraps a core-service gRPC connection.
func NewWorkspaceClient(connection grpc.ClientConnInterface) *WorkspaceClient {
	return &WorkspaceClient{client: workspacev1.NewWorkspaceServiceClient(connection)}
}

// GetHome forwards one workspace lookup.
func (client *WorkspaceClient) GetHome(
	ctx context.Context,
	request *emptypb.Empty,
) (*workspacev1.GetHomeResponse, error) {
	return client.client.GetHome(ctx, request)
}

// CreateBase forwards one new Base creation.
func (client *WorkspaceClient) CreateBase(
	ctx context.Context,
	request *workspacev1.CreateBaseRequest,
) (*workspacev1.CreateBaseResponse, error) {
	return client.client.CreateBase(ctx, request)
}
