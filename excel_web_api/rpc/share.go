// rpc/share.go：share gRPC client 的网关鉴权子集。
package rpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

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

// ShareManageRPC is the collaborator and link-sharing subset consumed by the gateway.
type ShareManageRPC interface {
	AddCollaborator(context.Context, *sharev1.AddCollaboratorRequest) (*sharev1.AddCollaboratorResponse, error)
	RemoveCollaborator(context.Context, *sharev1.RemoveCollaboratorRequest) (*emptypb.Empty, error)
	UpdateCollaboratorRole(context.Context, *sharev1.UpdateCollaboratorRoleRequest) (*sharev1.UpdateCollaboratorRoleResponse, error)
	ListCollaborators(context.Context, *sharev1.ListCollaboratorsRequest) (*sharev1.ListCollaboratorsResponse, error)
	GetLink(context.Context, *sharev1.GetLinkRequest) (*sharev1.GetLinkResponse, error)
	UpdateLink(context.Context, *sharev1.UpdateLinkRequest) (*sharev1.UpdateLinkResponse, error)
	ResetLink(context.Context, *sharev1.ResetLinkRequest) (*sharev1.ResetLinkResponse, error)
	CloseLink(context.Context, *sharev1.CloseLinkRequest) (*emptypb.Empty, error)
	ListOpenLinksByEnterprise(context.Context, *sharev1.ListOpenLinksByEnterpriseRequest) (*sharev1.ListOpenLinksByEnterpriseResponse, error)
	ForceCloseByManager(context.Context, *sharev1.ForceCloseByManagerRequest) (*emptypb.Empty, error)
}

var _ ShareManageRPC = (*ShareClient)(nil)

// AddCollaborator forwards one collaborator grant.
func (client *ShareClient) AddCollaborator(
	ctx context.Context,
	request *sharev1.AddCollaboratorRequest,
) (*sharev1.AddCollaboratorResponse, error) {
	return client.client.AddCollaborator(ctx, request)
}

// RemoveCollaborator forwards one collaborator revocation.
func (client *ShareClient) RemoveCollaborator(
	ctx context.Context,
	request *sharev1.RemoveCollaboratorRequest,
) (*emptypb.Empty, error) {
	return client.client.RemoveCollaborator(ctx, request)
}

// UpdateCollaboratorRole forwards one collaborator role update.
func (client *ShareClient) UpdateCollaboratorRole(
	ctx context.Context,
	request *sharev1.UpdateCollaboratorRoleRequest,
) (*sharev1.UpdateCollaboratorRoleResponse, error) {
	return client.client.UpdateCollaboratorRole(ctx, request)
}

// ListCollaborators forwards one collaborator page lookup.
func (client *ShareClient) ListCollaborators(
	ctx context.Context,
	request *sharev1.ListCollaboratorsRequest,
) (*sharev1.ListCollaboratorsResponse, error) {
	return client.client.ListCollaborators(ctx, request)
}

// GetLink forwards one link config lookup.
func (client *ShareClient) GetLink(
	ctx context.Context,
	request *sharev1.GetLinkRequest,
) (*sharev1.GetLinkResponse, error) {
	return client.client.GetLink(ctx, request)
}

// UpdateLink forwards one link config update.
func (client *ShareClient) UpdateLink(
	ctx context.Context,
	request *sharev1.UpdateLinkRequest,
) (*sharev1.UpdateLinkResponse, error) {
	return client.client.UpdateLink(ctx, request)
}

// ResetLink forwards one link reset.
func (client *ShareClient) ResetLink(
	ctx context.Context,
	request *sharev1.ResetLinkRequest,
) (*sharev1.ResetLinkResponse, error) {
	return client.client.ResetLink(ctx, request)
}

// CloseLink forwards one link close.
func (client *ShareClient) CloseLink(
	ctx context.Context,
	request *sharev1.CloseLinkRequest,
) (*emptypb.Empty, error) {
	return client.client.CloseLink(ctx, request)
}

// ListOpenLinksByEnterprise forwards one enterprise open-link page lookup.
func (client *ShareClient) ListOpenLinksByEnterprise(
	ctx context.Context,
	request *sharev1.ListOpenLinksByEnterpriseRequest,
) (*sharev1.ListOpenLinksByEnterpriseResponse, error) {
	return client.client.ListOpenLinksByEnterprise(ctx, request)
}

// ForceCloseByManager forwards one manager-forced link close.
func (client *ShareClient) ForceCloseByManager(
	ctx context.Context,
	request *sharev1.ForceCloseByManagerRequest,
) (*emptypb.Empty, error) {
	return client.client.ForceCloseByManager(ctx, request)
}
