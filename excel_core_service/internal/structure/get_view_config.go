package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
)

// GetViewConfig returns the current persisted view configuration.
func (service *Service) GetViewConfig(
	ctx context.Context,
	request *structurev1.GetViewConfigRequest,
) (*structurev1.GetViewConfigResponse, error) {
	principal, err := service.readPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	view, err := service.repository.ViewByID(ctx, request.GetViewId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	table, err := service.tableForReadTenancy(ctx, principal, view.TableID)
	if err != nil {
		return nil, mapStructureError(err)
	}
	if err := service.authorizeRead(ctx, principal, table.BaseID); err != nil {
		return nil, err
	}
	config, err := decodeViewConfig(view.Config)
	if err != nil {
		return nil, err
	}
	return &structurev1.GetViewConfigResponse{ViewConfig: config}, nil
}
