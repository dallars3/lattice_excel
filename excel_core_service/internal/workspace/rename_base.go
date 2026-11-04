// workspace/rename_base.go：一文件一接口。
package workspace

import (
	"context"

	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/deal"
)

// RenameBase changes a managed Base's trimmed name.
func (service *Service) RenameBase(
	ctx context.Context,
	request *workspacev1.RenameBaseRequest,
) (*workspacev1.RenameBaseResponse, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" || service.repository == nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	item, err := service.repository.BaseByID(ctx, request.GetBaseId())
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	if err := service.requireBaseManager(ctx, principal, item); err != nil {
		return nil, err
	}
	name, err := normalizeBaseName(request.GetName())
	if err != nil {
		return nil, err
	}
	exists, err := service.repository.BaseNameExists(
		ctx,
		item.EnterpriseID,
		name,
		item.ID,
	)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	if exists {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	if err := service.repository.RenameBase(
		ctx,
		item.ID,
		name,
		principal.GetUserId(),
		service.now(),
	); err != nil {
		return nil, mapWorkspaceError(err)
	}
	item, err = service.repository.BaseByID(ctx, item.ID)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	if item.OwnerID == principal.GetUserId() {
		item.MyRole = "owner"
	}
	return &workspacev1.RenameBaseResponse{
		Base: workspaceBaseToProto(item),
	}, nil
}
