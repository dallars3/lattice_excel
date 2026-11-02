// workspace/toggle_star.go：一文件一接口。
package workspace

import (
	"context"

	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/deal"
)

// ToggleStar idempotently sets the caller's star state for one visible Base.
func (service *Service) ToggleStar(
	ctx context.Context,
	request *workspacev1.ToggleStarRequest,
) (*workspacev1.ToggleStarResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" || service.repository == nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	role, err := service.repository.AccessRole(
		ctx,
		principal.GetUserId(),
		request.GetBaseId(),
	)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	if role == "" {
		return nil, rpcError(deal.CodePermissionDenied)
	}
	if err := service.repository.SetStar(
		ctx,
		principal.GetUserId(),
		request.GetBaseId(),
		request.GetStarred(),
		service.now(),
	); err != nil {
		return nil, mapWorkspaceError(err)
	}
	item, err := service.repository.BaseByID(ctx, request.GetBaseId())
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	item.MyRole = role
	item.Starred = request.GetStarred()
	return &workspacev1.ToggleStarResponse{Base: workspaceBaseToProto(item)}, nil
}
