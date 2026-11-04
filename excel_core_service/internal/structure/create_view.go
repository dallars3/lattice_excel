package structure

import (
	"context"
	"strings"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// CreateView creates a table view visible to every enterprise member.
func (service *Service) CreateView(
	ctx context.Context,
	request *structurev1.CreateViewRequest,
) (*structurev1.CreateViewResponse, error) {
	principal, _, err := service.memberPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	table, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		request.GetTableId(),
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	name, err := normalizeStructureName(request.GetName())
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	viewType := strings.TrimSpace(request.GetType())
	if viewType == "" {
		viewType = "table"
	}
	if viewType != "table" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	exists, err := service.repository.ViewNameExists(ctx, table.ID, name, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	count, err := service.repository.CountViews(ctx, table.ID)
	if err != nil {
		return nil, err
	}
	config, err := encodeViewConfig(&structurev1.ViewConfig{})
	if err != nil {
		return nil, err
	}
	id, err := idgen.New()
	if err != nil {
		return nil, err
	}
	view := base.StructureView{
		ID:        id,
		TableID:   table.ID,
		Name:      name,
		Type:      viewType,
		Config:    config,
		Position:  count,
		CreatedAt: service.now().UnixMilli(),
	}
	if err := service.repository.CreateView(ctx, view); err != nil {
		return nil, err
	}
	return &structurev1.CreateViewResponse{View: viewToProto(view)}, nil
}
