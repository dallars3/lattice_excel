package structure

import (
	"context"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// CreateTable creates one data table within a Base.
func (service *Service) CreateTable(
	ctx context.Context,
	request *structurev1.CreateTableRequest,
) (*structurev1.CreateTableResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	name, err := normalizeStructureName(request.GetName())
	if err != nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	count, err := service.repository.CountTables(ctx, principal.GetEnterpriseId(), request.GetBaseId())
	if err != nil {
		return nil, err
	}
	if count >= maxTablesPerBase {
		return nil, rpcError(deal.CodeCapacityLimit)
	}
	exists, err := service.repository.TableNameExists(
		ctx,
		principal.GetEnterpriseId(),
		request.GetBaseId(),
		name,
		"",
	)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	id, err := idgen.New()
	if err != nil {
		return nil, err
	}
	table := base.StructureTable{
		ID:           id,
		EnterpriseID: principal.GetEnterpriseId(),
		BaseID:       request.GetBaseId(),
		Name:         name,
		Position:     count,
		CreatedAt:    service.now().UnixMilli(),
	}
	if err := service.repository.CreateTable(ctx, table); err != nil {
		return nil, err
	}
	return &structurev1.CreateTableResponse{Table: tableToProto(table)}, nil
}
