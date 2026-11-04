// iam/admin_transfer_owner.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
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

// AdminTransferOwner replaces an enterprise owner without the current owner's session.
func (service *IamService) AdminTransferOwner(
	ctx context.Context,
	request *iamv1.AdminTransferOwnerRequest,
) (*emptypb.Empty, error) {
	if err := service.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if request == nil ||
		strings.TrimSpace(request.GetEnterpriseId()) == "" ||
		strings.TrimSpace(request.GetNewOwnerId()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()

	enterprise, err := service.repository.EnterpriseByIDForUpdate(
		ctx,
		tx,
		request.GetEnterpriseId(),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if enterprise.OwnerID == request.GetNewOwnerId() {
		return &emptypb.Empty{}, nil
	}
	currentOwner, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		enterprise.ID,
		enterprise.OwnerID,
	)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	successor, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		enterprise.ID,
		request.GetNewOwnerId(),
	)
	if errors.Is(err, sql.ErrNoRows) || err == nil && successor.Status != "active" {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateMemberRole(
		ctx,
		tx,
		enterprise.ID,
		currentOwner.UserID,
		"manager",
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateMemberRole(
		ctx,
		tx,
		enterprise.ID,
		successor.UserID,
		"owner",
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.SetEnterpriseOwner(
		ctx,
		tx,
		enterprise.ID,
		successor.UserID,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueEvent(ctx, tx, "enterprise.owner_transferred", struct {
		EnterpriseID string `json:"enterprise_id"`
		FromUserID   string `json:"from_user_id"`
		ToUserID     string `json:"to_user_id"`
	}{
		EnterpriseID: enterprise.ID,
		FromUserID:   currentOwner.UserID,
		ToUserID:     successor.UserID,
	}); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueAudit(
		ctx,
		tx,
		"AUDIT_ACTION_BASE_OWNERSHIP_TRANSFERRED",
		enterprise.ID,
		map[string]any{
			"from_user_id": currentOwner.UserID,
			"to_user_id":   successor.UserID,
		},
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
