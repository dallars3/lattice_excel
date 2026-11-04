// iam/reject_invite.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// RejectInvite applies the invitee-owned pending-to-rejected transition.
func (service *IamService) RejectInvite(
	ctx context.Context,
	request *iamv1.RejectInviteRequest,
) (*emptypb.Empty, error) {
	if request == nil || strings.TrimSpace(request.GetInviteId()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()

	invitation, err := service.repository.InvitationByIDForUpdate(
		ctx,
		tx,
		request.GetInviteId(),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if invitation.Status != "pending" {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	user, err := service.repository.UserByIDForUpdate(ctx, tx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if invitation.UserID != "" && invitation.UserID != user.ID ||
		!strings.EqualFold(invitation.Username, user.Username) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err := service.repository.UpdateInvitationStatus(
		ctx,
		tx,
		invitation.ID,
		"rejected",
		user.ID,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
