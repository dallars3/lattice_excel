package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
)

// UpdateViewConfig validates and immediately persists view configuration.
func (service *Service) UpdateViewConfig(
	ctx context.Context,
	request *structurev1.UpdateViewConfigRequest,
) (*structurev1.UpdateViewConfigResponse, error) {
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
	if err := service.validateViewConfig(ctx, view.TableID, request.GetViewConfig()); err != nil {
		return nil, err
	}
	configPayload, err := encodeViewConfig(request.GetViewConfig())
	if err != nil {
		return nil, err
	}
	view.Config = configPayload
	if err := service.repository.UpdateView(ctx, view); err != nil {
		return nil, mapStructureError(err)
	}
	config, err := decodeViewConfig(configPayload)
	if err != nil {
		return nil, err
	}
	return &structurev1.UpdateViewConfigResponse{ViewConfig: config}, nil
}
