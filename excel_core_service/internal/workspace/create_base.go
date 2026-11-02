// workspace/create_base.go：一文件一接口。
package workspace

import (
	"context"

	"excel-libs/idgen"
	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/base"
	"excel_core_service/deal"
)

// CreateBase creates one empty Base and its default table, fields and view.
func (service *Service) CreateBase(
	ctx context.Context,
	request *workspacev1.CreateBaseRequest,
) (*workspacev1.CreateBaseResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := service.requireManager(ctx, principal); err != nil {
		return nil, err
	}
	name, err := normalizeBaseName(request.GetName())
	if err != nil {
		return nil, err
	}
	if service.repository == nil || service.structureBootstrap == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	baseID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	item := base.WorkspaceBase{
		ID:             baseID,
		EnterpriseID:   principal.GetEnterpriseId(),
		OwnerID:        principal.GetUserId(),
		Name:           name,
		MyRole:         "owner",
		LastModifiedAt: now.UnixMilli(),
		LastModifier:   principal.GetUserId(),
		CreatedAt:      now.UnixMilli(),
	}
	if err := service.repository.CreateBase(ctx, item); err != nil {
		return nil, mapWorkspaceError(err)
	}
	if _, err := service.structureBootstrap(ctx, baseID); err != nil {
		_ = service.repository.DeleteBase(ctx, baseID)
		return nil, mapWorkspaceError(err)
	}
	item, err = service.repository.BaseByID(ctx, baseID)
	if err != nil {
		return nil, mapWorkspaceError(err)
	}
	item.MyRole = "owner"
	return &workspacev1.CreateBaseResponse{
		Base: workspaceBaseToProto(item),
	}, nil
}
