// rpc/workspace.go：workspace gRPC client 封装（文件名=模块名）。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	workspacev1 "excel/api/workspace/v1"
)

// WorkspaceRPC is the workspace surface consumed by the HTTP gateway.
type WorkspaceRPC interface {
	GetHome(context.Context, *emptypb.Empty) (*workspacev1.GetHomeResponse, error)
	SearchBases(context.Context, *workspacev1.SearchBasesRequest) (*workspacev1.SearchBasesResponse, error)
	CreateBase(context.Context, *workspacev1.CreateBaseRequest) (*workspacev1.CreateBaseResponse, error)
	RenameBase(context.Context, *workspacev1.RenameBaseRequest) (*workspacev1.RenameBaseResponse, error)
	DuplicateBase(context.Context, *workspacev1.DuplicateBaseRequest) (*workspacev1.DuplicateBaseResponse, error)
	DeleteBase(context.Context, *workspacev1.DeleteBaseRequest) (*emptypb.Empty, error)
	ToggleStar(context.Context, *workspacev1.ToggleStarRequest) (*workspacev1.ToggleStarResponse, error)
	TransferBase(context.Context, *workspacev1.TransferBaseRequest) (*emptypb.Empty, error)
}

// WorkspaceClient adapts the generated workspace gRPC client to WorkspaceRPC.
type WorkspaceClient struct {
	client workspacev1.WorkspaceServiceClient
}

var _ WorkspaceRPC = (*WorkspaceClient)(nil)

// NewWorkspaceClient wraps a core-service gRPC connection.
func NewWorkspaceClient(connection grpc.ClientConnInterface) *WorkspaceClient {
	return &WorkspaceClient{client: workspacev1.NewWorkspaceServiceClient(connection)}
}

// GetHome forwards a home page lookup.
func (client *WorkspaceClient) GetHome(
	ctx context.Context,
	request *emptypb.Empty,
) (*workspacev1.GetHomeResponse, error) {
	return client.client.GetHome(ctx, request)
}

// SearchBases forwards a Base search.
func (client *WorkspaceClient) SearchBases(
	ctx context.Context,
	request *workspacev1.SearchBasesRequest,
) (*workspacev1.SearchBasesResponse, error) {
	return client.client.SearchBases(ctx, request)
}

// CreateBase forwards a Base creation.
func (client *WorkspaceClient) CreateBase(
	ctx context.Context,
	request *workspacev1.CreateBaseRequest,
) (*workspacev1.CreateBaseResponse, error) {
	return client.client.CreateBase(ctx, request)
}

// RenameBase forwards a Base rename.
func (client *WorkspaceClient) RenameBase(
	ctx context.Context,
	request *workspacev1.RenameBaseRequest,
) (*workspacev1.RenameBaseResponse, error) {
	return client.client.RenameBase(ctx, request)
}

// DuplicateBase forwards a Base duplication.
func (client *WorkspaceClient) DuplicateBase(
	ctx context.Context,
	request *workspacev1.DuplicateBaseRequest,
) (*workspacev1.DuplicateBaseResponse, error) {
	return client.client.DuplicateBase(ctx, request)
}

// DeleteBase forwards a Base deletion.
func (client *WorkspaceClient) DeleteBase(
	ctx context.Context,
	request *workspacev1.DeleteBaseRequest,
) (*emptypb.Empty, error) {
	return client.client.DeleteBase(ctx, request)
}

// ToggleStar forwards a personal star toggle.
func (client *WorkspaceClient) ToggleStar(
	ctx context.Context,
	request *workspacev1.ToggleStarRequest,
) (*workspacev1.ToggleStarResponse, error) {
	return client.client.ToggleStar(ctx, request)
}

// TransferBase forwards Base ownership transfer.
func (client *WorkspaceClient) TransferBase(
	ctx context.Context,
	request *workspacev1.TransferBaseRequest,
) (*emptypb.Empty, error) {
	return client.client.TransferBase(ctx, request)
}
