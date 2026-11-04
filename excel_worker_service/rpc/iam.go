// rpc/iam.go：scheduler 调用的 core.iam 内部接口。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
)

// IAMRPC is the core IAM surface needed by scheduler cooldown jobs.
type IAMRPC interface {
	ExecuteDeactivation(
		context.Context,
		*iamv1.ExecuteDeactivationRequest,
	) (*emptypb.Empty, error)
	ExecuteEnterpriseDissolution(
		context.Context,
		*iamv1.ExecuteEnterpriseDissolutionRequest,
	) (*emptypb.Empty, error)
}

// IAMClient adapts the generated core IAM client.
type IAMClient struct {
	client iamv1.IamServiceClient
}

var _ IAMRPC = (*IAMClient)(nil)

// NewIAMClient wraps a core-service gRPC connection.
func NewIAMClient(connection grpc.ClientConnInterface) *IAMClient {
	return &IAMClient{client: iamv1.NewIamServiceClient(connection)}
}

// ExecuteDeactivation executes one due account deactivation.
func (client *IAMClient) ExecuteDeactivation(
	ctx context.Context,
	request *iamv1.ExecuteDeactivationRequest,
) (*emptypb.Empty, error) {
	return client.client.ExecuteDeactivation(ctx, request)
}

// ExecuteEnterpriseDissolution executes one due enterprise dissolution.
func (client *IAMClient) ExecuteEnterpriseDissolution(
	ctx context.Context,
	request *iamv1.ExecuteEnterpriseDissolutionRequest,
) (*emptypb.Empty, error) {
	return client.client.ExecuteEnterpriseDissolution(ctx, request)
}
