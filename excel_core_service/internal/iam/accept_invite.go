// iam/accept_invite.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// AcceptInvite joins the caller to the invitation's enterprise.
func (service *IamService) AcceptInvite(
	ctx context.Context,
	request *iamv1.AcceptInviteRequest,
) (*emptypb.Empty, error) {
	if request == nil || strings.TrimSpace(request.GetInviteId()) == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()

	invitation, err := service.repository.InvitationByIDForUpdate(
		ctx,
		tx,
		request.GetInviteId(),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if invitation.Status != "pending" {
		return nil, rpcError(deal.CodeConflict)
	}
	user, err := service.repository.UserByIDForUpdate(ctx, tx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if invitation.UserID != "" && invitation.UserID != user.ID {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if !strings.EqualFold(invitation.Username, user.Username) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if user.EnterpriseID != "" {
		return nil, rpcError(deal.CodeConflict)
	}

	member := base.IAMMember{
		EnterpriseID: invitation.EnterpriseID,
		UserID:       user.ID,
		Role:         "member",
		Status:       "active",
		JoinedAt:     service.now().UTC().UnixMilli(),
	}
	if err := service.repository.InsertMember(ctx, tx, member); err != nil {
		if errors.Is(err, base.ErrIAMMemberExists) {
			return nil, rpcError(deal.CodeConflict)
		}
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.SetUserEnterprise(ctx, tx, user.ID, invitation.EnterpriseID); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateInvitationStatus(
		ctx,
		tx,
		invitation.ID,
		"accepted",
		user.ID,
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.IncrementEnterpriseMemberCount(ctx, tx, invitation.EnterpriseID, 1); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
