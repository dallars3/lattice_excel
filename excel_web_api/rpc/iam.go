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

// IamEnterpriseRPC is the enterprise-management surface consumed by the HTTP gateway.
type IamEnterpriseRPC interface {
	RequestDeactivate(context.Context, *emptypb.Empty) (*iamv1.RequestDeactivateResponse, error)
	CancelDeactivate(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
	GetEnterprise(context.Context, *emptypb.Empty) (*iamv1.GetEnterpriseResponse, error)
	InviteMember(context.Context, *iamv1.InviteMemberRequest) (*iamv1.InviteMemberResponse, error)
	CancelInvite(context.Context, *iamv1.CancelInviteRequest) (*emptypb.Empty, error)
	ListInvites(context.Context, *iamv1.ListInvitesRequest) (*iamv1.ListInvitesResponse, error)
	ListMyInvitations(context.Context, *emptypb.Empty) (*iamv1.ListMyInvitationsResponse, error)
	AcceptInvite(context.Context, *iamv1.AcceptInviteRequest) (*emptypb.Empty, error)
	RejectInvite(context.Context, *iamv1.RejectInviteRequest) (*emptypb.Empty, error)
	ListMembers(context.Context, *iamv1.ListMembersRequest) (*iamv1.ListMembersResponse, error)
	UpdateMemberRole(context.Context, *iamv1.UpdateMemberRoleRequest) (*iamv1.UpdateMemberRoleResponse, error)
	DisableMember(context.Context, *iamv1.DisableMemberRequest) (*emptypb.Empty, error)
	EnableMember(context.Context, *iamv1.EnableMemberRequest) (*emptypb.Empty, error)
	TransferOwner(context.Context, *iamv1.TransferOwnerRequest) (*emptypb.Empty, error)
	CancelEnterpriseDissolution(context.Context, *emptypb.Empty) (*emptypb.Empty, error)
	GetEnterpriseDissolutionStatus(context.Context, *emptypb.Empty) (*iamv1.GetEnterpriseDissolutionStatusResponse, error)
	SearchUsers(context.Context, *iamv1.SearchUsersRequest) (*iamv1.SearchUsersResponse, error)
}

var _ IamEnterpriseRPC = (*IamClient)(nil)

// RequestDeactivate forwards one self-deactivation request.
func (client *IamClient) RequestDeactivate(
	ctx context.Context,
	request *emptypb.Empty,
) (*iamv1.RequestDeactivateResponse, error) {
	return client.client.RequestDeactivate(ctx, request)
}

// CancelDeactivate forwards one self-deactivation cancellation.
func (client *IamClient) CancelDeactivate(
	ctx context.Context,
	request *emptypb.Empty,
) (*emptypb.Empty, error) {
	return client.client.CancelDeactivate(ctx, request)
}

// GetEnterprise forwards the enterprise profile lookup.
func (client *IamClient) GetEnterprise(
	ctx context.Context,
	request *emptypb.Empty,
) (*iamv1.GetEnterpriseResponse, error) {
	return client.client.GetEnterprise(ctx, request)
}

// InviteMember forwards one member-invite request.
func (client *IamClient) InviteMember(
	ctx context.Context,
	request *iamv1.InviteMemberRequest,
) (*iamv1.InviteMemberResponse, error) {
	return client.client.InviteMember(ctx, request)
}

// CancelInvite forwards one pending-invite cancellation.
func (client *IamClient) CancelInvite(
	ctx context.Context,
	request *iamv1.CancelInviteRequest,
) (*emptypb.Empty, error) {
	return client.client.CancelInvite(ctx, request)
}

// ListInvites forwards one pending-invite page query.
func (client *IamClient) ListInvites(
	ctx context.Context,
	request *iamv1.ListInvitesRequest,
) (*iamv1.ListInvitesResponse, error) {
	return client.client.ListInvites(ctx, request)
}

// ListMyInvitations forwards the caller's invitation list query.
func (client *IamClient) ListMyInvitations(
	ctx context.Context,
	request *emptypb.Empty,
) (*iamv1.ListMyInvitationsResponse, error) {
	return client.client.ListMyInvitations(ctx, request)
}

// AcceptInvite forwards one invitation acceptance.
func (client *IamClient) AcceptInvite(
	ctx context.Context,
	request *iamv1.AcceptInviteRequest,
) (*emptypb.Empty, error) {
	return client.client.AcceptInvite(ctx, request)
}

// RejectInvite forwards one invitation rejection.
func (client *IamClient) RejectInvite(
	ctx context.Context,
	request *iamv1.RejectInviteRequest,
) (*emptypb.Empty, error) {
	return client.client.RejectInvite(ctx, request)
}

// ListMembers forwards one enterprise member page query.
func (client *IamClient) ListMembers(
	ctx context.Context,
	request *iamv1.ListMembersRequest,
) (*iamv1.ListMembersResponse, error) {
	return client.client.ListMembers(ctx, request)
}

// UpdateMemberRole forwards one member role update.
func (client *IamClient) UpdateMemberRole(
	ctx context.Context,
	request *iamv1.UpdateMemberRoleRequest,
) (*iamv1.UpdateMemberRoleResponse, error) {
	return client.client.UpdateMemberRole(ctx, request)
}

// DisableMember forwards one member disable request.
func (client *IamClient) DisableMember(
	ctx context.Context,
	request *iamv1.DisableMemberRequest,
) (*emptypb.Empty, error) {
	return client.client.DisableMember(ctx, request)
}

// EnableMember forwards one member enable request.
func (client *IamClient) EnableMember(
	ctx context.Context,
	request *iamv1.EnableMemberRequest,
) (*emptypb.Empty, error) {
	return client.client.EnableMember(ctx, request)
}

// TransferOwner forwards one enterprise ownership transfer.
func (client *IamClient) TransferOwner(
	ctx context.Context,
	request *iamv1.TransferOwnerRequest,
) (*emptypb.Empty, error) {
	return client.client.TransferOwner(ctx, request)
}

// CancelEnterpriseDissolution forwards one dissolution cancellation.
func (client *IamClient) CancelEnterpriseDissolution(
	ctx context.Context,
	request *emptypb.Empty,
) (*emptypb.Empty, error) {
	return client.client.CancelEnterpriseDissolution(ctx, request)
}

// GetEnterpriseDissolutionStatus forwards the dissolution status lookup.
func (client *IamClient) GetEnterpriseDissolutionStatus(
	ctx context.Context,
	request *emptypb.Empty,
) (*iamv1.GetEnterpriseDissolutionStatusResponse, error) {
	return client.client.GetEnterpriseDissolutionStatus(ctx, request)
}

// SearchUsers forwards one user search.
func (client *IamClient) SearchUsers(
	ctx context.Context,
	request *iamv1.SearchUsersRequest,
) (*iamv1.SearchUsersResponse, error) {
	return client.client.SearchUsers(ctx, request)
}
