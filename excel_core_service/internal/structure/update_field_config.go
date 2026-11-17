package structure

import (
	"context"
	"fmt"

	"excel_core_service/deal"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

// UpdateFieldConfig replaces type-specific field configuration.
func (service *Service) UpdateFieldConfig(
	ctx context.Context,
	request *structurev1.UpdateFieldConfigRequest,
) (*structurev1.UpdateFieldConfigResponse, error) {
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
	fieldType := structurev1.FieldType(structurev1.FieldType_value[field.Type])
	input := map[string]any{}
	if request.GetConfigMap() != nil {
		input = request.GetConfigMap().AsMap()
	}
	config, options, err := service.normalizeFieldConfig(ctx, table, fieldType, input)
	if err != nil {
		return nil, deal.RPCError(1001)
	}
	payload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	updated := field
	updated.Config = payload
	if updated.Type == "FIELD_TYPE_FORMULA" ||
		updated.Type == "FIELD_TYPE_RELATION" ||
		updated.Type == "FIELD_TYPE_LOOKUP" {
		fields, err := service.repository.AllFields(ctx)
		if err != nil {
			return nil, err
		}
		proposed := make([]base.StructureField, 0, len(fields))
		for _, existing := range fields {
			if existing.ID == updated.ID {
				proposed = append(proposed, updated)
			} else {
				proposed = append(proposed, existing)
			}
		}
		if err := validateProposedGraph(ctx, service.calcValidator, proposed); err != nil {
			return nil, err
		}
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.UpdateFieldTx(ctx, tx, updated, options); err != nil {
		return nil, mapStructureError(err)
	}
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		updated.ID,
		structureChangeFieldUpdated,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit UpdateFieldConfig: %w", err)
	}
	projected, err := fieldToProto(updated)
	if err != nil {
		return nil, err
	}
	return &structurev1.UpdateFieldConfigResponse{Field: projected}, nil
}
