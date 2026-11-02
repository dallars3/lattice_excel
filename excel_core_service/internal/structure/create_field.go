package structure

import (
	"context"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// CreateField creates one typed field after strict config and graph validation.
func (service *Service) CreateField(
	ctx context.Context,
	request *structurev1.CreateFieldRequest,
) (*structurev1.CreateFieldResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetTableId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
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
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	fieldType, ok := fieldTypeString(request.GetType())
	if !ok {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	count, err := service.repository.CountFields(ctx, table.ID)
	if err != nil {
		return nil, err
	}
	if count >= maxFieldsPerTable {
		return nil, rpcError(deal.CodeCapacityLimit)
	}
	exists, err := service.repository.FieldNameExists(ctx, table.ID, name, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	input := map[string]any{}
	if request.GetConfigMap() != nil {
		input = request.GetConfigMap().AsMap()
	}
	config, options, err := service.normalizeFieldConfig(ctx, table, request.GetType(), input)
	if err != nil {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	configPayload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	fieldID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	field := base.StructureField{
		ID:        fieldID,
		TableID:   table.ID,
		Name:      name,
		Type:      fieldType,
		Config:    configPayload,
		IsPrimary: count == 0,
		Position:  count,
		CreatedAt: service.now().UnixMilli(),
	}
	if field.Type == "FIELD_TYPE_FORMULA" ||
		field.Type == "FIELD_TYPE_RELATION" ||
		field.Type == "FIELD_TYPE_LOOKUP" {
		allFields, err := service.repository.AllFields(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateProposedGraph(
			ctx,
			service.calcValidator,
			append(allFields, field),
		); err != nil {
			return nil, err
		}
	}
	if err := service.repository.CreateField(ctx, field, options); err != nil {
		return nil, err
	}
	projected, err := fieldToProto(field)
	if err != nil {
		return nil, err
	}
	return &structurev1.CreateFieldResponse{Field: projected}, nil
}
