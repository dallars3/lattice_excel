package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ConvertField previews or confirms one supported field type conversion.
func (service *Service) ConvertField(
	ctx context.Context,
	request *structurev1.ConvertFieldRequest,
) (*structurev1.ConvertFieldResponse, error) {
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
	targetType, ok := fieldTypeString(request.GetTargetType())
	if !ok || !canConvertFieldType(field.Type, targetType) {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	input := map[string]any{}
	if request.GetConfigMap() != nil {
		input = request.GetConfigMap().AsMap()
	}
	config, options, err := service.normalizeFieldConfig(
		ctx,
		table,
		request.GetTargetType(),
		input,
	)
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	configPayload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	affected, cleared, err := service.conversionCounts(
		ctx,
		field.TableID,
		field.ID,
		targetType,
	)
	if err != nil {
		return nil, err
	}
	converted := field
	converted.Type = targetType
	converted.Config = configPayload
	if !request.GetConfirm() {
		return &structurev1.ConvertFieldResponse{
			AffectedCount: int32(affected),
			ClearedCount:  int32(cleared),
		}, nil
	}
	if err := service.repository.UpdateField(ctx, converted, options); err != nil {
		return nil, err
	}
	projected, err := fieldToProto(converted)
	if err != nil {
		return nil, err
	}
	return &structurev1.ConvertFieldResponse{
		Field:         projected,
		AffectedCount: int32(affected),
		ClearedCount:  int32(cleared),
	}, nil
}

func (service *Service) conversionCounts(
	ctx context.Context,
	tableID, fieldID, targetType string,
) (int, int, error) {
	if counter, ok := service.impact.(FieldConversionImpactCounter); ok {
		affected, cleared, err := counter.FieldConversionImpact(ctx, tableID, fieldID, targetType)
		return affected, cleared, err
	}
	if service.impact == nil {
		return 0, 0, deal.RPCError(deal.CodeServiceUnavailable)
	}
	affected, err := service.impact.AffectedRecords(ctx, tableID, fieldID)
	if err != nil {
		return 0, 0, err
	}
	return affected, 0, nil
}

func canConvertFieldType(source, target string) bool {
	if source == target {
		return true
	}
	switch source {
	case "FIELD_TYPE_FORMULA", "FIELD_TYPE_RELATION", "FIELD_TYPE_LOOKUP":
		return false
	}
	if target == "FIELD_TYPE_TEXT" {
		return true
	}
	switch source {
	case "FIELD_TYPE_TEXT":
		return oneOf(target,
			"FIELD_TYPE_NUMBER",
			"FIELD_TYPE_SELECT",
			"FIELD_TYPE_MULTISELECT",
			"FIELD_TYPE_DATE",
		)
	case "FIELD_TYPE_SELECT":
		return target == "FIELD_TYPE_MULTISELECT"
	case "FIELD_TYPE_MULTISELECT":
		return target == "FIELD_TYPE_SELECT"
	default:
		return false
	}
}
