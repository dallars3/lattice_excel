// iam/list_my_invitations.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// ListMyInvitations returns pending invitations matched by user ID or username.
func (service *IamService) ListMyInvitations(
	ctx context.Context,
	_ *emptypb.Empty,
) (*iamv1.ListMyInvitationsResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	user, err := service.repository.UserByID(ctx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	invitations, err := service.repository.ListUserInvitations(ctx, user.ID, user.Username)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	response := &iamv1.ListMyInvitationsResponse{
		InvitationList: make([]*iamv1.Invitation, 0, len(invitations)),
	}
	for _, invitation := range invitations {
		response.InvitationList = append(response.InvitationList, protoInvitation(invitation))
	}
	return response, nil
}
