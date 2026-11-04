package structure

import (
	"context"
	"excel_core_service/deal"

	structurev1 "excel/api/structure/v1"
)

// RenameTable renames a table after an exact trimmed-name duplicate check.
func (service *Service) RenameTable(
	ctx context.Context,
	request *structurev1.RenameTableRequest,
) (*structurev1.RenameTableResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
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
		return nil, deal.RPCError(1001)
	}
	exists, err := service.repository.TableNameExists(
		ctx,
		principal.GetEnterpriseId(),
		table.BaseID,
		name,
		table.ID,
	)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, deal.RPCError(1001)
	}
	if err := service.repository.RenameTable(ctx, table.EnterpriseID, table.ID, name); err != nil {
		return nil, mapStructureError(err)
	}
	table.Name = name
	return &structurev1.RenameTableResponse{Table: tableToProto(table)}, nil
}
