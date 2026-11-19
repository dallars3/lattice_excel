package structure

import (
	"context"
	"fmt"

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
	// 主字段保护（P0 TC-040-02，与 DeleteField 的主字段禁删同类）：主字段承载
	// 行标识，类型转换被 UI 隐藏且 API 一律拒绝（预览与确认都不允许）。
	if field.IsPrimary {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
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
	// 目标为公式/关联/引用时与 UpdateFieldConfig 同口径校验依赖图，转换不得
	// 引入环或悬空引用（否则回填作业只会以作业失败收场）。
	if isComputedField(converted.Type) {
		fields, err := service.repository.AllFields(ctx)
		if err != nil {
			return nil, err
		}
		proposed := make([]base.StructureField, 0, len(fields))
		for _, existing := range fields {
			if existing.ID == converted.ID {
				proposed = append(proposed, converted)
			} else {
				proposed = append(proposed, existing)
			}
		}
		if err := validateProposedGraph(ctx, service.calcValidator, proposed); err != nil {
			return nil, err
		}
	}
	// 值迁移只服务可写目标：转公式/引用后列值由计算物化/读路径实时物化，
	// 原值保留在记录 cells 中不参与读取，无需迁移改写。
	if !needsCalcBackfill(targetType) {
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
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.UpdateFieldTx(ctx, tx, converted, options); err != nil {
		return nil, mapStructureError(err)
	}
	// N5 票 03：转出公式/引用/关联时按删除字段级联同口径清理物化列，残留
	// 投影与结构变更原子消失。
	if fieldLeavesComputedTypes(field.Type, targetType) {
		if err := service.cleanupFieldMaterializationTx(ctx, tx, converted.ID); err != nil {
			return nil, err
		}
	}
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		converted.ID,
		structureChangeFieldConverted,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	// N5 缺陷：转成公式/引用后存量记录的列值需回填——同事务入队 calc 回填
	// 作业，由 calc 消费者分批幂等物化。
	if needsCalcBackfill(targetType) {
		if err := service.enqueueCalcBackfill(ctx, tx, table, converted.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ConvertField: %w", err)
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
	// 转成公式/引用（N5 票 03）：任意可写类型均可转换，原值保留在记录 cells
	// 中（列转为只读计算语义后不参与读取）；关联字段是用户数据（链接 id），
	// 转换语义未定义，保持不可转。
	if needsCalcBackfill(target) {
		switch source {
		case "FIELD_TYPE_FORMULA", "FIELD_TYPE_LOOKUP", "FIELD_TYPE_RELATION":
			return false
		}
		return true
	}
	// 转出公式/引用（N5 票 03）：计算值不迁移，转为可编辑文本列起步，
	// 物化列按删除字段级联同口径清理。
	if needsCalcBackfill(source) {
		return target == "FIELD_TYPE_TEXT"
	}
	// 关联字段承载用户链接数据，任何方向的转换语义未定义，保持不可转。
	if source == "FIELD_TYPE_RELATION" {
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

// fieldLeavesComputedTypes 报告一次确认转换是否让字段离开公式/引用/关联族：
// 是则物化列按删除字段级联同口径清理。同族互转（如公式改表达式）不清理——
// 物化值随回填作业以同字段幂等覆盖。判断经 isComputedField 单一谓词
// （双轴评审第 3 项），不散写 FIELD_TYPE_* 比较。
func fieldLeavesComputedTypes(source, target string) bool {
	return isComputedField(source) && !isComputedField(target)
}
