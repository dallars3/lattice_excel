// iam/invite_member.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package iam

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"excel-libs/idgen"
	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// InviteMember creates pending invitations by enterprise-scoped username.
func (service *IamService) InviteMember(
	ctx context.Context,
	request *iamv1.InviteMemberRequest,
) (*iamv1.InviteMemberResponse, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	operator, err := service.activeMember(ctx, principal)
	if err != nil {
		return nil, err
	}
	if !canManageMembers(operator) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if len(request.GetUsernameList()) == 0 {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	enterprise, err := service.repository.EnterpriseByID(ctx, operator.EnterpriseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	seen := make(map[string]struct{}, len(request.GetUsernameList()))
	for _, username := range request.GetUsernameList() {
		username = strings.TrimSpace(username)
		if !validUsername(username) {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		key := strings.ToLower(username)
		if _, exists := seen[key]; exists {
			return nil, deal.RPCError(deal.CodeConflict)
		}
		seen[key] = struct{}{}
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()

	now := service.now().UTC().UnixMilli()
	invitations := make([]base.IAMInvitation, 0, len(request.GetUsernameList()))
	for _, rawUsername := range request.GetUsernameList() {
		username := strings.TrimSpace(rawUsername)
		userID := ""
		user, err := service.repository.UserByUsernameForUpdate(ctx, tx, username)
		switch {
		case err == nil:
			// 票 03（N7 收尾）：注册自动建企后几乎所有用户都已有自己的企业——
			// 邀请不再要求「用户无企业归属」，按用户名可邀请任何活跃用户；仅拒绝
			// 非活跃目标与已是本企业活跃成员的目标。用户企业字段与归属他企业的
			// 成员行都不是障碍（接受邀请按成员行跨企业并入）。
			if user.Status != "active" {
				return nil, deal.RPCErrorMessage(deal.CodeConflict, "该用户当前不可被邀请")
			}
			if _, err := service.repository.ActiveMemberRoleTx(ctx, tx, operator.EnterpriseID, user.ID); err == nil {
				return nil, deal.RPCErrorMessage(deal.CodeConflict, "该用户已是本企业成员")
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, deal.RPCError(deal.CodeServiceUnavailable)
			}
			username = user.Username
			userID = user.ID
		case errors.Is(err, sql.ErrNoRows):
		default:
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}

		invitation, err := service.repository.InvitationByEnterpriseAndUsernameForUpdate(
			ctx,
			tx,
			operator.EnterpriseID,
			username,
		)
		switch {
		case err == nil:
			if invitation.Status == "pending" {
				return nil, deal.RPCError(deal.CodeConflict)
			}
			invitation.Username = username
			invitation.UserID = userID
			invitation.Status = "pending"
			invitation.CreatedAt = now
			if err := service.repository.ReopenInvitation(ctx, tx, invitation); err != nil {
				return nil, deal.RPCError(deal.CodeServiceUnavailable)
			}
		case errors.Is(err, sql.ErrNoRows):
			invitation.ID, err = idgen.New()
			if err != nil {
				return nil, deal.RPCError(deal.CodeServiceUnavailable)
			}
			invitation = base.IAMInvitation{
				ID:             invitation.ID,
				EnterpriseID:   operator.EnterpriseID,
				EnterpriseName: enterprise.Name,
				Username:       username,
				UserID:         userID,
				Role:           "member",
				Status:         "pending",
				CreatedAt:      now,
			}
			if err := service.repository.InsertInvitation(ctx, tx, invitation); err != nil {
				if errors.Is(err, base.ErrIAMInvitationExists) {
					return nil, deal.RPCError(deal.CodeConflict)
				}
				return nil, deal.RPCError(deal.CodeServiceUnavailable)
			}
		default:
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		if err := service.enqueueEvent(ctx, tx, "invite.created", struct {
			EnterpriseID string `json:"enterprise_id"`
			Username     string `json:"username"`
			InviteID     string `json:"invite_id"`
		}{
			EnterpriseID: operator.EnterpriseID,
			Username:     username,
			InviteID:     invitation.ID,
		}); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		invitations = append(invitations, invitation)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	response := &iamv1.InviteMemberResponse{
		InvitationList: make([]*iamv1.Invitation, 0, len(invitations)),
	}
	for _, invitation := range invitations {
		response.InvitationList = append(response.InvitationList, protoInvitation(invitation))
	}
	return response, nil
}
