// iam/update_member_role.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/deal"
)

// UpdateMemberRole changes a non-owner membership role under the owner invariant.
func (service *IamService) UpdateMemberRole(
	ctx context.Context,
	request *iamv1.UpdateMemberRoleRequest,
) (*iamv1.UpdateMemberRoleResponse, error) {
	if request == nil || strings.TrimSpace(request.GetUserId()) == "" ||
		(request.GetRole() != iamv1.MemberRole_MEMBER_ROLE_MANAGER &&
			request.GetRole() != iamv1.MemberRole_MEMBER_ROLE_MEMBER) {
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
	target, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		operator.EnterpriseID,
		request.GetUserId(),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if target.UserID == operator.UserID || target.Role == "owner" {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	ownerCount, err := service.repository.CountActiveOwners(ctx, tx, operator.EnterpriseID)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if ownerCount < 1 {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	role := memberRoleDB(request.GetRole())
	if err := service.repository.UpdateMemberRole(
		ctx,
		tx,
		target.EnterpriseID,
		target.UserID,
		role,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	target.Role = role
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &iamv1.UpdateMemberRoleResponse{Member: protoMember(target)}, nil
}

func memberRoleDB(role iamv1.MemberRole) string {
	switch role {
	case iamv1.MemberRole_MEMBER_ROLE_OWNER:
		return "owner"
	case iamv1.MemberRole_MEMBER_ROLE_MANAGER:
		return "manager"
	case iamv1.MemberRole_MEMBER_ROLE_MEMBER:
		return "member"
	default:
		return ""
	}
}
