// structure/validation.go：结构名称、字段配置与依赖图校验。
package structure

import (
	"context"
	"errors"
	"excel_core_service/deal"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

const (
	maxStructureNameRunes = 30
	maxTablesPerBase      = 50
	maxFieldsPerTable     = 100
)

func normalizeStructureName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	count := utf8.RuneCountInString(name)
	if count < 1 || count > maxStructureNameRunes {
		return "", errors.New("名称长度必须为 1-30 字符")
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", errors.New("名称包含非法控制字符")
		}
	}
	return name, nil
}

func (service *Service) normalizeFieldConfig(
	ctx context.Context,
	table base.StructureTable,
	fieldType structurev1.FieldType,
	input map[string]any,
) (map[string]any, []base.StructureOption, error) {
	if input == nil {
		input = map[string]any{}
	}
	switch fieldType {
	case structurev1.FieldType_FIELD_TYPE_TEXT,
		structurev1.FieldType_FIELD_TYPE_ATTACHMENT,
		structurev1.FieldType_FIELD_TYPE_URL:
		if err := requireOnlyKeys(input); err != nil {
			return nil, nil, err
		}
		return map[string]any{}, nil, nil
	case structurev1.FieldType_FIELD_TYPE_NUMBER:
		return normalizeNumberConfig(input)
	case structurev1.FieldType_FIELD_TYPE_SELECT,
		structurev1.FieldType_FIELD_TYPE_MULTISELECT:
		return normalizeSelectConfig(input)
	case structurev1.FieldType_FIELD_TYPE_DATE:
		return normalizeDateConfig(input)
	case structurev1.FieldType_FIELD_TYPE_PERSON:
		return normalizePersonConfig(input)
	case structurev1.FieldType_FIELD_TYPE_FORMULA:
		return normalizeFormulaConfig(input)
	case structurev1.FieldType_FIELD_TYPE_RELATION:
		return service.normalizeRelationConfig(ctx, table, input)
	case structurev1.FieldType_FIELD_TYPE_LOOKUP:
		return normalizeLookupConfig(input)
	default:
		return nil, nil, errors.New("字段类型非法")
	}
}

func normalizeNumberConfig(input map[string]any) (map[string]any, []base.StructureOption, error) {
	if err := requireOnlyKeys(input, "format", "decimals", "currency", "thousands"); err != nil {
		return nil, nil, err
	}
	config := map[string]any{}
	if value, exists := input["format"]; exists {
		format, ok := value.(string)
		if !ok || !oneOf(format, "int", "decimal", "percent", "currency") {
			return nil, nil, errors.New("number.format 非法")
		}
		config["format"] = format
	}
	if value, exists := input["decimals"]; exists {
		decimals, ok := integerValue(value)
		if !ok || decimals < 0 || decimals > 4 {
			return nil, nil, errors.New("number.decimals 必须为 0-4 的整数")
		}
		config["decimals"] = decimals
	}
	if value, exists := input["currency"]; exists {
		currency, ok := value.(string)
		if !ok || !oneOf(currency, "¥", "$", "€", "£") {
			return nil, nil, errors.New("number.currency 非法")
		}
		config["currency"] = currency
	}
	if value, exists := input["thousands"]; exists {
		thousands, ok := value.(bool)
		if !ok {
			return nil, nil, errors.New("number.thousands 必须为布尔值")
		}
		config["thousands"] = thousands
	}
	return config, nil, nil
}

func normalizeSelectConfig(input map[string]any) (map[string]any, []base.StructureOption, error) {
	if err := requireOnlyKeys(input, "option_list", "allow_create"); err != nil {
		return nil, nil, err
	}
	rawOptions := []any{}
	if value, exists := input["option_list"]; exists {
		var ok bool
		rawOptions, ok = value.([]any)
		if !ok {
			return nil, nil, errors.New("option_list 必须为数组")
		}
	}
	options, normalized, err := normalizeOptions(rawOptions)
	if err != nil {
		return nil, nil, err
	}
	allowCreate := false
	if value, exists := input["allow_create"]; exists {
		var ok bool
		allowCreate, ok = value.(bool)
		if !ok {
			return nil, nil, errors.New("allow_create 必须为布尔值")
		}
	}
	return map[string]any{
		"option_list":  normalized,
		"allow_create": allowCreate,
	}, options, nil
}

func normalizeOptions(rawOptions []any) ([]base.StructureOption, []any, error) {
	options := make([]base.StructureOption, 0, len(rawOptions))
	normalized := make([]any, 0, len(rawOptions))
	names := make(map[string]struct{}, len(rawOptions))
	ids := make(map[string]struct{}, len(rawOptions))
	for index, raw := range rawOptions {
		option, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("option_list[%d] 必须为对象", index)
		}
		if err := requireOnlyKeys(option, "id", "name", "color"); err != nil {
			return nil, nil, fmt.Errorf("option_list[%d]: %w", index, err)
		}
		name, err := normalizeStructureName(stringValue(option["name"]))
		if err != nil {
			return nil, nil, fmt.Errorf("option_list[%d].name: %w", index, err)
		}
		nameKey := strings.ToLower(name)
		if _, exists := names[nameKey]; exists {
			return nil, nil, errors.New("同一字段内选项名不可重名")
		}
		names[nameKey] = struct{}{}
		color := stringValue(option["color"])
		if utf8.RuneCountInString(color) > 32 {
			return nil, nil, errors.New("选项颜色超过 32 字符")
		}
		optionID := stringValue(option["id"])
		if optionID == "" {
			optionID, err = idgen.New()
			if err != nil {
				return nil, nil, fmt.Errorf("generate option id: %w", err)
			}
		}
		if len(optionID) > 26 || strings.ContainsAny(optionID, " \t\r\n") {
			return nil, nil, errors.New("选项 ID 非法")
		}
		if _, exists := ids[optionID]; exists {
			return nil, nil, errors.New("选项 ID 不可重复")
		}
		ids[optionID] = struct{}{}
		options = append(options, base.StructureOption{
			OptionID: optionID,
			Name:     name,
			Color:    color,
			Position: index,
		})
		normalized = append(normalized, map[string]any{
			"id":    optionID,
			"name":  name,
			"color": color,
		})
	}
	return options, normalized, nil
}

func normalizeDateConfig(input map[string]any) (map[string]any, []base.StructureOption, error) {
	if err := requireOnlyKeys(input, "include_time", "format"); err != nil {
		return nil, nil, err
	}
	config := map[string]any{}
	if value, exists := input["include_time"]; exists {
		includeTime, ok := value.(bool)
		if !ok {
			return nil, nil, errors.New("date.include_time 必须为布尔值")
		}
		config["include_time"] = includeTime
	}
	if value, exists := input["format"]; exists {
		format, ok := value.(string)
		if !ok || !oneOf(format, "YYYY/MM/DD", "YYYY-MM-DD", "YYYY年M月D日") {
			return nil, nil, errors.New("date.format 非法")
		}
		config["format"] = format
	}
	return config, nil, nil
}

func normalizePersonConfig(input map[string]any) (map[string]any, []base.StructureOption, error) {
	if err := requireOnlyKeys(input, "allow_multiple"); err != nil {
		return nil, nil, err
	}
	config := map[string]any{}
	if value, exists := input["allow_multiple"]; exists {
		allowMultiple, ok := value.(bool)
		if !ok {
			return nil, nil, errors.New("person.allow_multiple 必须为布尔值")
		}
		config["allow_multiple"] = allowMultiple
	}
	return config, nil, nil
}

func normalizeFormulaConfig(input map[string]any) (map[string]any, []base.StructureOption, error) {
	if err := requireOnlyKeys(input, "expression"); err != nil {
		return nil, nil, err
	}
	expression, ok := input["expression"].(string)
	if !ok || strings.TrimSpace(expression) == "" {
		return nil, nil, errors.New("formula.expression 不可为空")
	}
	return map[string]any{"expression": strings.TrimSpace(expression)}, nil, nil
}

func (service *Service) normalizeRelationConfig(
	ctx context.Context,
	table base.StructureTable,
	input map[string]any,
) (map[string]any, []base.StructureOption, error) {
	if err := requireOnlyKeys(input, "target_table_id", "allow_multiple"); err != nil {
		return nil, nil, err
	}
	targetTableID, ok := input["target_table_id"].(string)
	if !ok || strings.TrimSpace(targetTableID) == "" {
		return nil, nil, errors.New("relation.target_table_id 不可为空")
	}
	target, err := service.repository.TableByID(ctx, table.EnterpriseID, targetTableID)
	if err != nil || target.BaseID != table.BaseID {
		return nil, nil, errors.New("relation.target_table_id 必须是本 Base 内数据表")
	}
	config := map[string]any{"target_table_id": targetTableID}
	if value, exists := input["allow_multiple"]; exists {
		allowMultiple, ok := value.(bool)
		if !ok {
			return nil, nil, errors.New("relation.allow_multiple 必须为布尔值")
		}
		config["allow_multiple"] = allowMultiple
	}
	return config, nil, nil
}

func normalizeLookupConfig(input map[string]any) (map[string]any, []base.StructureOption, error) {
	if err := requireOnlyKeys(input, "relation_field_id", "target_field_id"); err != nil {
		return nil, nil, err
	}
	relationFieldID, relationOK := input["relation_field_id"].(string)
	targetFieldID, targetOK := input["target_field_id"].(string)
	if !relationOK || strings.TrimSpace(relationFieldID) == "" ||
		!targetOK || strings.TrimSpace(targetFieldID) == "" {
		return nil, nil, errors.New("lookup 必须指定 relation_field_id 和 target_field_id")
	}
	return map[string]any{
		"relation_field_id": relationFieldID,
		"target_field_id":   targetFieldID,
	}, nil, nil
}

func validateProposedGraph(
	ctx context.Context,
	validator CalcValidator,
	fields []base.StructureField,
) error {
	definitions, err := definitionsFromStructureFields(fields)
	if err != nil {
		return deal.RPCError(1001)
	}
	if err := validator.ValidateDependencyGraph(ctx, definitions); err != nil {
		return deal.RPCError(1001)
	}
	return nil
}

func requireOnlyKeys(input map[string]any, allowed ...string) error {
	if len(allowed) == 0 {
		if len(input) != 0 {
			return errors.New("该字段类型不接受配置参数")
		}
		return nil
	}
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := allowedKeys[key]; !ok {
			return fmt.Errorf("未知配置参数 %q", key)
		}
	}
	return nil
}

func integerValue(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) ||
		math.Trunc(number) != number ||
		number < math.MinInt32 || number > math.MaxInt32 {
		return 0, false
	}
	return int(number), true
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
