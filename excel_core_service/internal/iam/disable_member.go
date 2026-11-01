// iam/disable_member.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
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

// DisableMember revokes access and reassigns the member's Base ownership.
func (service *IamService) DisableMember(
	ctx context.Context,
	request *iamv1.DisableMemberRequest,
) (*emptypb.Empty, error) {
	if request == nil || strings.TrimSpace(request.GetUserId()) == "" {
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

	operator, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		principal.GetEnterpriseId(),
		principal.GetUserId(),
	)
	if errors.Is(err, sql.ErrNoRows) ||
		err == nil && (operator.Status != "active" || !canManageMembers(operator)) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	target, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		operator.EnterpriseID,
		request.GetUserId(),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if target.Role == "owner" {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if target.Status == "disabled" {
		return &emptypb.Empty{}, nil
	}

	enterprise, err := service.repository.EnterpriseByIDForUpdate(ctx, tx, operator.EnterpriseID)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateMemberStatus(
		ctx,
		tx,
		target.EnterpriseID,
		target.UserID,
		"disabled",
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	// TODO(09): the workspace implementation will atomically move every Base
	// and collaborator asset to the enterprise owner through this seam.
	if service.bases != nil {
		if err := service.bases.ReassignAllBases(ctx, target.UserID, enterprise.OwnerID); err != nil {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
	}
	if err := service.enqueueEvent(ctx, tx, "member.disabled", struct {
		EnterpriseID string `json:"enterprise_id"`
		UserID       string `json:"user_id"`
		Operator     string `json:"operator"`
	}{
		EnterpriseID: target.EnterpriseID,
		UserID:       target.UserID,
		Operator:     operator.UserID,
	}); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}
