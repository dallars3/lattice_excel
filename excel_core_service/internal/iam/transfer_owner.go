// iam/transfer_owner.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
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

// TransferOwner atomically swaps the enterprise owner and member roles.
func (service *IamService) TransferOwner(
	ctx context.Context,
	request *iamv1.TransferOwnerRequest,
) (*emptypb.Empty, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil ||
		!request.GetConfirm() ||
		strings.TrimSpace(request.GetSuccessorId()) == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if request.GetSuccessorId() == principal.GetUserId() {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()

	operator, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		principal.GetEnterpriseId(),
		principal.GetUserId(),
	)
	if errors.Is(err, sql.ErrNoRows) ||
		err == nil && (operator.Status != "active" || operator.Role != "owner") {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if _, err := service.repository.EnterpriseByIDForUpdate(ctx, tx, operator.EnterpriseID); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	successor, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		operator.EnterpriseID,
		request.GetSuccessorId(),
	)
	if errors.Is(err, sql.ErrNoRows) ||
		err == nil && (successor.Status != "active" || successor.UserID == operator.UserID) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateMemberRole(
		ctx,
		tx,
		operator.EnterpriseID,
		operator.UserID,
		"manager",
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateMemberRole(
		ctx,
		tx,
		successor.EnterpriseID,
		successor.UserID,
		"owner",
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.SetEnterpriseOwner(
		ctx,
		tx,
		operator.EnterpriseID,
		successor.UserID,
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueEvent(ctx, tx, "enterprise.owner_transferred", struct {
		EnterpriseID string `json:"enterprise_id"`
		FromUserID   string `json:"from_user_id"`
		ToUserID     string `json:"to_user_id"`
	}{
		EnterpriseID: operator.EnterpriseID,
		FromUserID:   operator.UserID,
		ToUserID:     successor.UserID,
	}); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
