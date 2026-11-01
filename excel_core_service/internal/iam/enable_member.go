// iam/enable_member.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
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

// EnableMember restores a disabled member's original enterprise membership.
func (service *IamService) EnableMember(
	ctx context.Context,
	request *iamv1.EnableMemberRequest,
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
	if target.Status == "active" {
		return &emptypb.Empty{}, nil
	}
	if err := service.repository.UpdateMemberStatus(
		ctx,
		tx,
		target.EnterpriseID,
		target.UserID,
		"active",
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueEvent(ctx, tx, "member.enabled", struct {
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
