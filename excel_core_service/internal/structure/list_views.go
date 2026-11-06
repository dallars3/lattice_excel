package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
)

// ListViews lists views visible to every enterprise member.
func (service *Service) ListViews(
	ctx context.Context,
	request *structurev1.ListViewsRequest,
) (*structurev1.ListViewsResponse, error) {
	principal, err := service.readPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	table, collaboratorPath, err := service.tableForReadTenancy(ctx, principal, request.GetTableId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	if err := service.authorizeRead(ctx, principal, table.BaseID, collaboratorPath); err != nil {
		return nil, err
	}
	views, err := service.repository.ViewsByTable(ctx, request.GetTableId())
	if err != nil {
		return nil, err
	}
	response := &structurev1.ListViewsResponse{
		ViewList: make([]*structurev1.View, 0, len(views)),
	}
	for _, view := range views {
		response.ViewList = append(response.ViewList, viewToProto(view))
	}
	return response, nil
}
