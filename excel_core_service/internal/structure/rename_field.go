package structure

import (
	"context"
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
	if _, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		field.TableID,
	); err != nil {
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
		return nil, deal.RPCError(1001)
	}
	if err := service.repository.RenameField(ctx, field.ID, name); err != nil {
		return nil, mapStructureError(err)
	}
	field.Name = name
	projected, err := fieldToProto(field)
	if err != nil {
		return nil, err
	}
	return &structurev1.RenameFieldResponse{Field: projected}, nil
}
