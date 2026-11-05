// share/update_collaborator_role.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"
	"database/sql"
	"errors"

	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// UpdateCollaboratorRole changes a Base role without allowing self-demotion.
// 目标以条目 id（=user_id）寻址，用户名兜底；可填写（filler）为合法目标角色。
func (service *Service) UpdateCollaboratorRole(
	ctx context.Context,
	request *sharev1.UpdateCollaboratorRoleRequest,
) (*sharev1.UpdateCollaboratorRoleResponse, error) {
	if request == nil || request.GetBaseId() == "" || request.GetUserId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	role := normalizeRole(request.GetRole().String())
	if role == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := service.requireAction(
		ctx,
		request.GetBaseId(),
		sharev1.Action_ACTION_MANAGE_SHARE,
	)
	if err != nil {
		return nil, err
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	collaborators, err := service.repository.ListCollaboratorsForUpdate(ctx, tx, request.GetBaseId())
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	target, ok, err := service.resolveCollaboratorTarget(ctx, collaborators, request.GetUserId())
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if !ok {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if target.UserID == principal.GetUserId() && role != "manager" {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if !target.Internal && role == "manager" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if target.Role == "manager" && role != "manager" && managerCount(collaborators) <= 1 {
		remaining, err := service.hasRemainingManager(
			ctx,
			request.GetBaseId(),
			collaborators,
			target.UserID,
			role,
		)
		if err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
		if !remaining {
			return nil, deal.RPCError(deal.CodePermissionDenied)
		}
	}
	enterpriseID, err := service.bases.EnterpriseID(ctx, request.GetBaseId())
	if err != nil || enterpriseID == "" {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.repository.UpdateCollaboratorRole(
		ctx,
		tx,
		request.GetBaseId(),
		target.UserID,
		role,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	changed := target.Role != role
	target.Role = role
	if changed {
		if err := service.enqueueShareChanged(
			ctx,
			tx,
			request.GetBaseId(),
			enterpriseID,
			"updated",
			target.UserID,
			role,
		); err != nil {
			return nil, deal.RPCError(deal.CodeServiceUnavailable)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	profiles, err := service.profilesFor(ctx, []string{target.UserID})
	if err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &sharev1.UpdateCollaboratorRoleResponse{
		Collaborator: collaboratorEntry(target, profiles),
	}, nil
}

func (service *Service) hasRemainingManager(
	ctx context.Context,
	baseID string,
	collaborators []base.ShareCollaborator,
	changedUserID, resultingRole string,
) (bool, error) {
	if service.baseOwner != nil {
		ownerID, err := service.baseOwner.OwnerID(ctx, baseID)
		switch {
		case err == nil && ownerID != "":
			return true, nil
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return false, err
		}
	}
	for _, collaborator := range collaborators {
		role := collaborator.Role
		if collaborator.UserID == changedUserID {
			role = resultingRole
		}
		if role == "manager" {
			return true, nil
		}
	}
	return false, nil
}
