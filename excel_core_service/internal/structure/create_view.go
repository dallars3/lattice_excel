package structure

import (
	"context"
	"fmt"
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
		return nil, deal.RPCError(deal.CodeConflict)
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
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.CreateViewTx(ctx, tx, view); err != nil {
		return nil, mapStructureError(err)
	}
	// 缺陷 #18（W-11）：视图写成功即与写同事务发布结构事件。
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		"",
		structureChangeViewCreated,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit CreateView: %w", err)
	}
	return &structurev1.CreateViewResponse{View: viewToProto(view)}, nil
}
