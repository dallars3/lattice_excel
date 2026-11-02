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
	config, err := decodeViewConfig(view.Config)
	if err != nil {
		return nil, err
	}
	return &structurev1.GetViewConfigResponse{ViewConfig: config}, nil
}
