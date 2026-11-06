package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// ConvertField previews or confirms one supported field type conversion.
// 预览（confirm=false/缺省）只统计受影响记录数与将清空值数，不写库；
// 确认后先迁移记录值（失败则不产生任何写入），再落结构行（缺陷 #26 / H-38）。
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
	inheritSelectOptions(field, request.GetTargetType(), input)
	config, options, err := service.normalizeFieldConfig(
		ctx,
		table,
		request.GetTargetType(),
		input,
	)
	if err != nil {
		return nil, mapFieldConfigError(err)
	}
	configPayload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	targetOptionNames := optionNamesFromNormalizedConfig(config)
	affected, cleared, err := service.conversionCounts(
		ctx,
		field.TableID,
		field.ID,
		targetType,
		targetOptionNames,
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
	applier, ok := service.impact.(FieldConversionApplier)
	if !ok {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	if err := applier.ApplyFieldConversion(
		ctx,
		field.TableID,
		field.ID,
		targetType,
		targetOptionNames,
	); err != nil {
		return nil, err
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
	targetOptionNames map[string]string,
) (int, int, error) {
	if counter, ok := service.impact.(FieldConversionImpactCounter); ok {
		affected, cleared, err := counter.FieldConversionImpact(
			ctx,
			tableID,
			fieldID,
			targetType,
			targetOptionNames,
		)
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

// inheritSelectOptions keeps option ids stable through select conversions:
// 目标为单选/多选且请求未带 option_list 时，继承源字段的选项（含 allow_create），
// 由 normalizeOptions 保留既有 id，避免转换后既有单元格引用悬空。
func inheritSelectOptions(
	field base.StructureField,
	targetType structurev1.FieldType,
	input map[string]any,
) {
	if targetType != structurev1.FieldType_FIELD_TYPE_SELECT &&
		targetType != structurev1.FieldType_FIELD_TYPE_MULTISELECT {
		return
	}
	if _, exists := input["option_list"]; exists {
		return
	}
	source, err := base.DecodeStructureConfig(field.Config)
	if err != nil {
		return
	}
	options, exists := source["option_list"].([]any)
	if !exists || len(options) == 0 {
		return
	}
	input["option_list"] = options
	if _, exists := input["allow_create"]; !exists {
		if allowCreate, exists := source["allow_create"]; exists {
			input["allow_create"] = allowCreate
		}
	}
}

// optionNamesFromNormalizedConfig 提取归一化目标配置的选项 id→名 映射，
// 供转换统计与值迁移按选项名精确匹配。
func optionNamesFromNormalizedConfig(config map[string]any) map[string]string {
	names := map[string]string{}
	rawOptions, _ := config["option_list"].([]any)
	for _, raw := range rawOptions {
		option, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		optionID, _ := option["id"].(string)
		name, _ := option["name"].(string)
		if optionID != "" && name != "" {
			names[optionID] = name
		}
	}
	return names
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
