package structure

import (
	"context"
	"fmt"

	"excel_core_service/deal"

	structurev1 "excel/api/structure/v1"
)

// RenameField renames a field after an exact trimmed-name duplicate check.
func (service *Service) RenameField(
	ctx context.Context,
	request *structurev1.RenameFieldRequest,
) (*structurev1.RenameFieldResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	field, err := service.repository.FieldByID(ctx, request.GetFieldId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	table, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		field.TableID,
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	name, err := normalizeStructureName(request.GetName())
	if err != nil {
		return nil, deal.RPCError(1001)
	}
	exists, err := service.repository.FieldNameExists(ctx, field.TableID, name, field.ID)
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
	if err := service.repository.RenameFieldTx(ctx, tx, field.ID, name); err != nil {
		return nil, mapStructureError(err)
	}
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		field.ID,
		structureChangeFieldRenamed,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit RenameField: %w", err)
	}
	field.Name = name
	projected, err := fieldToProto(field)
	if err != nil {
		return nil, err
	}
	return &structurev1.RenameFieldResponse{Field: projected}, nil
}
