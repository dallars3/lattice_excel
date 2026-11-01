// share/update_collaborator_role.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"

	sharev1 "excel/api/share/v1"
	"excel_core_service/deal"
)

// UpdateCollaboratorRole changes a Base role without allowing self-demotion.
func (service *Service) UpdateCollaboratorRole(
	ctx context.Context,
	request *sharev1.UpdateCollaboratorRoleRequest,
) (*sharev1.UpdateCollaboratorRoleResponse, error) {
	if request == nil || request.GetBaseId() == "" || request.GetUserId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	role := normalizeRole(request.GetRole().String())
	if role != "viewer" && role != "editor" && role != "manager" {
		return nil, rpcError(deal.CodeInvalidArgument)
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
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback() }()
	collaborators, err := service.repository.ListCollaboratorsForUpdate(ctx, tx, request.GetBaseId())
	if err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	target, ok := findCollaborator(collaborators, request.GetUserId())
	if !ok {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if request.GetUserId() == principal.GetUserId() && role != "manager" {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if !target.Internal && role == "manager" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if target.Role == "manager" && role != "manager" && managerCount(collaborators) <= 1 {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err := service.repository.UpdateCollaboratorRole(
		ctx,
		tx,
		request.GetBaseId(),
		request.GetUserId(),
		role,
	); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	target.Role = role
	if err := tx.Commit(); err != nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	return &sharev1.UpdateCollaboratorRoleResponse{
		Collaborator: protoCollaborator(target),
	}, nil
}
