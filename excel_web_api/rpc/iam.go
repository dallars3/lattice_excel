// rpc/iam.go：该模块 gRPC client 封装（文件名=模块名）。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
)

// IamRPC is the ticket-04 subset used by the HTTP gateway.
type IamRPC interface {
	Register(context.Context, *iamv1.RegisterRequest) (*iamv1.RegisterResponse, error)
	Login(context.Context, *iamv1.LoginRequest) (*iamv1.LoginResponse, error)
	Logout(context.Context, *iamv1.LogoutRequest) (*emptypb.Empty, error)
	ExchangeSession(context.Context, *iamv1.ExchangeSessionRequest) (*iamv1.ExchangeSessionResponse, error)
	ChangePassword(context.Context, *iamv1.ChangePasswordRequest) (*emptypb.Empty, error)
	GetMe(context.Context, *emptypb.Empty) (*iamv1.GetMeResponse, error)
	UpdateProfile(context.Context, *iamv1.UpdateProfileRequest) (*iamv1.UpdateProfileResponse, error)
}

// IamClient adapts the generated gRPC client to the gateway's IamRPC seam.
type IamClient struct {
	client iamv1.IamServiceClient
}

// NewIamClient wraps a core-service gRPC connection.
func NewIamClient(connection grpc.ClientConnInterface) *IamClient {
	return &IamClient{client: iamv1.NewIamServiceClient(connection)}
}

// Register forwards an account registration request.
func (client *IamClient) Register(
	ctx context.Context,
	request *iamv1.RegisterRequest,
) (*iamv1.RegisterResponse, error) {
	return client.client.Register(ctx, request)
}

// Login forwards a credential login request.
func (client *IamClient) Login(ctx context.Context, request *iamv1.LoginRequest) (*iamv1.LoginResponse, error) {
	return client.client.Login(ctx, request)
}

// Logout forwards a session invalidation request.
func (client *IamClient) Logout(ctx context.Context, request *iamv1.LogoutRequest) (*emptypb.Empty, error) {
	return client.client.Logout(ctx, request)
}

// ExchangeSession resolves an opaque ticket into a Principal.
func (client *IamClient) ExchangeSession(
	ctx context.Context,
	request *iamv1.ExchangeSessionRequest,
) (*iamv1.ExchangeSessionResponse, error) {
	return client.client.ExchangeSession(ctx, request)
}

// ChangePassword forwards a password change request.
func (client *IamClient) ChangePassword(
	ctx context.Context,
	request *iamv1.ChangePasswordRequest,
) (*emptypb.Empty, error) {
	return client.client.ChangePassword(ctx, request)
}

// GetMe forwards the authenticated account lookup.
func (client *IamClient) GetMe(ctx context.Context, request *emptypb.Empty) (*iamv1.GetMeResponse, error) {
	return client.client.GetMe(ctx, request)
}

// UpdateProfile forwards an authenticated profile update.
func (client *IamClient) UpdateProfile(
	ctx context.Context,
	request *iamv1.UpdateProfileRequest,
) (*iamv1.UpdateProfileResponse, error) {
	return client.client.UpdateProfile(ctx, request)
}
