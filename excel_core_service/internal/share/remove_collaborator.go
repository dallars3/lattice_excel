// share/remove_collaborator.go：一文件一接口。细则见《excel_core_service-技术方案》§13/§14。
package share

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// RemoveCollaborator removes one grant while protecting the caller and final manager.
func (service *Service) RemoveCollaborator(
	ctx context.Context,
	request *sharev1.RemoveCollaboratorRequest,
) (*emptypb.Empty, error) {
	if request == nil || request.GetBaseId() == "" || request.GetUserId() == "" {
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
	target, ok := findCollaborator(collaborators, request.GetUserId())
	if !ok {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if request.GetUserId() == principal.GetUserId() {
		return nil, deal.RPCError(deal.CodePermissionDenied)
	}
	if target.Role == "manager" && managerCount(collaborators) <= 1 {
		remaining, err := service.hasRemainingManager(
			ctx,
			request.GetBaseId(),
			collaborators,
			request.GetUserId(),
			"",
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
	if err := service.repository.DeleteCollaborator(
		ctx,
		tx,
		request.GetBaseId(),
		request.GetUserId(),
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := service.enqueueShareChanged(
		ctx,
		tx,
		request.GetBaseId(),
		enterpriseID,
		"removed",
		request.GetUserId(),
		target.Role,
	); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	return &emptypb.Empty{}, nil
}

func findCollaborator(collaborators []base.ShareCollaborator, userID string) (base.ShareCollaborator, bool) {
	for _, collaborator := range collaborators {
		if collaborator.UserID == userID {
			return collaborator, true
		}
	}
	return base.ShareCollaborator{}, false
}

func managerCount(collaborators []base.ShareCollaborator) int {
	count := 0
	for _, collaborator := range collaborators {
		if collaborator.Role == "manager" {
			count++
		}
	}
	return count
}
