package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
)

// RenameView renames one view for any enterprise member.
func (service *Service) RenameView(
	ctx context.Context,
	request *structurev1.RenameViewRequest,
) (*structurev1.RenameViewResponse, error) {
	principal, _, err := service.memberPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := service.repository.ViewByID(ctx, request.GetViewId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	if _, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		view.TableID,
	); err != nil {
		return nil, mapStructureError(err)
	}
	name, err := normalizeStructureName(request.GetName())
	if err != nil {
		return nil, rpcError(1001)
	}
	exists, err := service.repository.ViewNameExists(ctx, view.TableID, name, view.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, rpcError(1001)
	}
	if err := service.repository.RenameView(ctx, view.ID, name); err != nil {
		return nil, mapStructureError(err)
	}
	view.Name = name
	return &structurev1.RenameViewResponse{View: viewToProto(view)}, nil
}
