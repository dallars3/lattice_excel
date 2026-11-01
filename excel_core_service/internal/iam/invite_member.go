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
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	operator, err := service.activeMember(ctx, principal)
	if err != nil {
		return nil, err
	}
	if !canManageMembers(operator) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if len(request.GetUsernameList()) == 0 {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	enterprise, err := service.repository.EnterpriseByID(ctx, operator.EnterpriseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	seen := make(map[string]struct{}, len(request.GetUsernameList()))
	for _, username := range request.GetUsernameList() {
		username = strings.TrimSpace(username)
		if !validUsername(username) {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		key := strings.ToLower(username)
		if _, exists := seen[key]; exists {
			return nil, rpcError(deal.CodeConflict)
		}
		seen[key] = struct{}{}
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
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
			if user.EnterpriseID != "" {
				return nil, rpcError(deal.CodeConflict)
			}
			username = user.Username
			userID = user.ID
		case errors.Is(err, sql.ErrNoRows):
		default:
			return nil, rpcError(deal.CodeServiceUnavailable)
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
				return nil, rpcError(deal.CodeConflict)
			}
			invitation.Username = username
			invitation.UserID = userID
			invitation.Status = "pending"
			invitation.CreatedAt = now
			if err := service.repository.ReopenInvitation(ctx, tx, invitation); err != nil {
				return nil, rpcError(deal.CodeServiceUnavailable)
			}
		case errors.Is(err, sql.ErrNoRows):
			invitation.ID, err = idgen.New()
			if err != nil {
				return nil, rpcError(deal.CodeServiceUnavailable)
			}
			invitation = base.IAMInvitation{
				ID:             invitation.ID,
				EnterpriseID:   operator.EnterpriseID,
				EnterpriseName: enterprise.Name,
				Username:       username,
				UserID:         userID,
				Status:         "pending",
				CreatedAt:      now,
			}
			if err := service.repository.InsertInvitation(ctx, tx, invitation); err != nil {
				if errors.Is(err, base.ErrIAMInvitationExists) {
					return nil, rpcError(deal.CodeConflict)
				}
				return nil, rpcError(deal.CodeServiceUnavailable)
			}
		default:
			return nil, rpcError(deal.CodeServiceUnavailable)
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
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		invitations = append(invitations, invitation)
	}
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	response := &iamv1.InviteMemberResponse{
		InvitationList: make([]*iamv1.Invitation, 0, len(invitations)),
	}
	for _, invitation := range invitations {
		response.InvitationList = append(response.InvitationList, protoInvitation(invitation))
	}
	return response, nil
}
