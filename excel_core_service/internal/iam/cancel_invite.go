// iam/cancel_invite.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
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

// CancelInvite withdraws one pending invitation from the caller's enterprise.
func (service *IamService) CancelInvite(
	ctx context.Context,
	request *iamv1.CancelInviteRequest,
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

	operator, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		principal.GetEnterpriseId(),
		principal.GetUserId(),
	)
	if errors.Is(err, sql.ErrNoRows) ||
		err == nil && (operator.Status != "active" || !canManageMembers(operator)) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
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
	if invitation.EnterpriseID != operator.EnterpriseID {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if invitation.Status != "pending" {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	if err := service.repository.UpdateInvitationStatus(
		ctx,
		tx,
		invitation.ID,
		"canceled",
		invitation.UserID,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueEvent(ctx, tx, "invite.canceled", struct {
		EnterpriseID string `json:"enterprise_id"`
		Username     string `json:"username"`
		InviteID     string `json:"invite_id"`
	}{
		EnterpriseID: invitation.EnterpriseID,
		Username:     invitation.Username,
		InviteID:     invitation.ID,
	}); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
