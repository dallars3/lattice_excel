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

// AcceptInvite joins the caller to the invitation's enterprise. 票 03（N7 收尾）：
// 接受 = 目标企业新增成员行（角色按邀请授予），不改写用户企业字段、不迁移既有
// Base——用户以成员行跨企业归属，自有企业与其数据访问不受影响。
func (service *IamService) AcceptInvite(
	ctx context.Context,
	request *iamv1.AcceptInviteRequest,
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
	user, err := service.repository.UserByIDForUpdate(ctx, tx, principal.GetUserId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodeUnauthenticated)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if invitation.UserID != "" && invitation.UserID != user.ID {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if !strings.EqualFold(invitation.Username, user.Username) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if invitation.Status == "accepted" && invitation.UserID == user.ID {
		// 幂等重入：同一邀请被同一账号重复接受按成功空操作处理（不建行、不计数）。
		if err := tx.Commit(); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		return &emptypb.Empty{}, nil
	}
	if invitation.Status != "pending" {
		return nil, deal.RPCError(deal.CodeConflict)
	}

	// 幂等并入：目标企业已有该账号的成员行时不再插入。活跃成员行视为已加入
	// （邀请置为 accepted，成员计数不变）；非活跃（已被移除）仍按冲突拒绝。
	existing, err := service.repository.MemberForUpdate(
		ctx,
		tx,
		invitation.EnterpriseID,
		user.ID,
	)
	switch {
	case err == nil:
		if existing.Status != "active" {
			return nil, deal.RPCError(deal.CodeConflict)
		}
		if err := service.repository.UpdateInvitationStatus(
			ctx,
			tx,
			invitation.ID,
			"accepted",
			user.ID,
		); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		if err := tx.Commit(); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		return &emptypb.Empty{}, nil
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}

	member := base.IAMMember{
		EnterpriseID: invitation.EnterpriseID,
		UserID:       user.ID,
		Role:         invitationGrantedRole(invitation.Role),
		Status:       "active",
		JoinedAt:     service.now().UTC().UnixMilli(),
	}
	if err := service.repository.InsertMember(ctx, tx, member); err != nil {
		if errors.Is(err, base.ErrIAMMemberExists) {
			return nil, deal.RPCError(deal.CodeConflict)
		}
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateInvitationStatus(
		ctx,
		tx,
		invitation.ID,
		"accepted",
		user.ID,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.IncrementEnterpriseMemberCount(ctx, tx, invitation.EnterpriseID, 1); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}

// invitationGrantedRole maps the invitation's granted role to the member row
// role. 列默认 member（000020 迁移回填存量行）；当前 API 固定授予 member（proto
// 未开放角色授予），manager 预留给后续授予路径，其余值一律按 member 兜底。
func invitationGrantedRole(role string) string {
	if role == "member" || role == "manager" {
		return role
	}
	return "member"
}
