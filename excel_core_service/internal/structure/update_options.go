package structure

import (
	"context"
	"fmt"
	"strings"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// UpdateOptions replaces select options and the allow-create switch.
func (service *Service) UpdateOptions(
	ctx context.Context,
	request *structurev1.UpdateOptionsRequest,
) (*structurev1.UpdateOptionsResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	field, err := service.repository.FieldByID(ctx, request.GetFieldId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	if field.Type != "FIELD_TYPE_SELECT" && field.Type != "FIELD_TYPE_MULTISELECT" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	table, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		field.TableID,
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	optionList := mergeSameNameOptions(request.GetOptionList(), field.Config)
	config, options, err := normalizeSelectConfig(map[string]any{
		"option_list":  optionList,
		"allow_create": request.GetAllowCreate(),
	})
	if err != nil {
		return nil, mapFieldConfigError(err)
	}
	payload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	field.Config = payload
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.UpdateFieldTx(ctx, tx, field, options); err != nil {
		return nil, mapStructureError(err)
	}
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		field.ID,
		structureChangeFieldOptionsUpdated,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit UpdateOptions: %w", err)
	}
	projected, err := fieldToProto(field)
	if err != nil {
		return nil, err
	}
	return &structurev1.UpdateOptionsResponse{Field: projected}, nil
}

// mergeSameNameOptions 把请求选项按「去空格、忽略大小写」后的名称归一：重复项
// 不新增选项（首个生效），其余按序保留（H-40：同名归一而不是失败）。未带 id
// 的选项按名称回填既有 id，保证选项 id 跨更新稳定——记录值引用选项 id，维护
// 改名/调色/排序不得使其失效。
func mergeSameNameOptions(
	requestOptions []*structurev1.FieldOption,
	currentConfig []byte,
) []any {
	existingIDs := existingOptionIDsByName(currentConfig)
	merged := make([]any, 0, len(requestOptions))
	seen := make(map[string]struct{}, len(requestOptions))
	for _, option := range requestOptions {
		name := strings.TrimSpace(option.GetName())
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		optionID := strings.TrimSpace(option.GetId())
		if optionID == "" {
			optionID = existingIDs[key]
		}
		merged = append(merged, map[string]any{
			"id":    optionID,
			"name":  name,
			"color": option.GetColor(),
		})
	}
	return merged
}

// existingOptionIDsByName extracts normalized-name → option-id from the
// persisted select config；配置损坏时按空表处理（新 id 由 normalizeOptions 生成）。
func existingOptionIDsByName(config []byte) map[string]string {
	ids := make(map[string]string)
	decoded, err := base.DecodeStructureConfig(config)
	if err != nil {
		return ids
	}
	rawOptions, ok := decoded["option_list"].([]any)
	if !ok {
		return ids
	}
	for _, raw := range rawOptions {
		option, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := option["name"].(string)
		optionID, _ := option["id"].(string)
		name = strings.TrimSpace(name)
		if name == "" || optionID == "" {
			continue
		}
		ids[strings.ToLower(name)] = optionID
	}
	return ids
}
