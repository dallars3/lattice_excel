package structure

import (
	"context"
	"fmt"

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
		return nil, deal.RPCError(deal.CodeConflict)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.RenameTableTx(ctx, tx, table.EnterpriseID, table.ID, name); err != nil {
		return nil, mapStructureError(err)
	}
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		"",
		structureChangeTableRenamed,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit RenameTable: %w", err)
	}
	table.Name = name
	return &structurev1.RenameTableResponse{Table: tableToProto(table)}, nil
}
