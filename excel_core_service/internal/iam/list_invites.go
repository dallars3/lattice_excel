// iam/list_invites.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// ListInvites returns one pending invitation keyset page for the enterprise.
func (service *IamService) ListInvites(
	ctx context.Context,
	request *iamv1.ListInvitesRequest,
) (*iamv1.ListInvitesResponse, error) {
	if request == nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	operator, err := service.activeMember(ctx, principal)
	if err != nil {
		return nil, err
	}
	if !canManageMembers(operator) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	limit := int(request.GetLimit())
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	invitations, err := service.repository.ListInvitations(
		ctx,
		operator.EnterpriseID,
		request.GetCursor(),
		limit+1,
	)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	hasMore := len(invitations) > limit
	if hasMore {
		invitations = invitations[:limit]
	}
	response := &iamv1.ListInvitesResponse{
		InvitationList: make([]*iamv1.Invitation, 0, len(invitations)),
		HasMore:        hasMore,
	}
	for _, invitation := range invitations {
		response.InvitationList = append(response.InvitationList, protoInvitation(invitation))
	}
	if hasMore && len(invitations) > 0 {
		response.NextCursor = invitations[len(invitations)-1].ID
	}
	return response, nil
}
