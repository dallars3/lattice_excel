// records/field_conversion.go：字段类型转换的记录值影响面统计与确认迁移。
// 转换规则遵循 PRD 7.1：任意→文本取显示值（日期按 YYYY-MM-DD、选项取选项名）；
// 文本→数字去千分位/货币符号后解析、失败置空；文本→日期按常见格式解析、失败
// 置空；文本→单选/多选按选项名精确匹配、不自动新建；单选→多选单值变单元素；
// 多选→单选保留第一个选项。结构域经 structure.FieldConversionApplier seam 调用，
// 本文件只读写 excel_records/excel_records_histories（CODING_STANDARDS §2.5 表所有权）。
package records

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// convertDateLayouts 是文本→日期的解析格式（PRD 7.1）。
var convertDateLayouts = []string{"2006-01-02", "2006/01/02", "2006年1月2日"}

// convertNumberStripper 去除千分位与常见货币符号（PRD 7.1）。
var convertNumberStripper = strings.NewReplacer(
	",", "", "¥", "", "$", "", "€", "", "£", "", " ", "", "\t", "",
)

// convertDisplayZone 用于日期显示值文本的时区（与记录域一致取上海）。
var convertDisplayZone = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return location
}()

// FieldConversionAdapter adapts the records persistence to the structure
// conversion seams: preview counting and confirmed value migration.
type FieldConversionAdapter struct {
	records   *base.RecordsRepository
	structure *base.StructureRepository
}

// NewFieldConversionAdapter creates the conversion adapter used by structure.
func NewFieldConversionAdapter(
	recordsRepository *base.RecordsRepository,
	structureRepository *base.StructureRepository,
) *FieldConversionAdapter {
	return &FieldConversionAdapter{
		records:   recordsRepository,
		structure: structureRepository,
	}
}

// AffectedRecords counts active records holding a non-empty value on the field.
func (adapter *FieldConversionAdapter) AffectedRecords(
	ctx context.Context,
	tableID, fieldID string,
) (int, error) {
	field, err := adapter.structure.FieldByID(ctx, fieldID)
	if err != nil {
		return 0, fmt.Errorf("load counted field %s: %w", fieldID, err)
	}
	if field.TableID != tableID {
		return 0, nil
	}
	recordsList, err := adapter.records.RecordsByTable(ctx, tableID, "")
	if err != nil {
		return 0, err
	}
	affected := 0
	for _, record := range recordsList {
		if !isClearedCell(record.Cells[fieldID]) {
			affected++
		}
	}
	return affected, nil
}

// FieldConversionImpact counts the records a conversion would touch and the
// values it would clear, applying the same rules as ApplyFieldConversion.
func (adapter *FieldConversionAdapter) FieldConversionImpact(
	ctx context.Context,
	tableID, fieldID, targetType string,
	targetOptionNames map[string]string,
) (affected, cleared int, err error) {
	field, err := adapter.structure.FieldByID(ctx, fieldID)
	if err != nil {
		return 0, 0, fmt.Errorf("load counted field %s: %w", fieldID, err)
	}
	if field.TableID != tableID {
		return 0, 0, nil
	}
	sourceOptionNames := optionNamesFromFieldConfig(field.Config)
	recordsList, err := adapter.records.RecordsByTable(ctx, tableID, "")
	if err != nil {
		return 0, 0, err
	}
	for _, record := range recordsList {
		value := record.Cells[fieldID]
		if isClearedCell(value) {
			continue
		}
		converted, changed := convertFieldValue(
			value,
			field.Type,
			targetType,
			sourceOptionNames,
			targetOptionNames,
		)
		if !changed {
			continue
		}
		affected++
		if isClearedCell(converted) {
			cleared++
		}
	}
	return affected, cleared, nil
}

// ApplyFieldConversion migrates stored values for one confirmed conversion in
// a single transaction: each touched record is version-bumped and its before/
// after values are appended to the record history for recovery.
func (adapter *FieldConversionAdapter) ApplyFieldConversion(
	ctx context.Context,
	tableID, fieldID, targetType string,
	targetOptionNames map[string]string,
) error {
	field, err := adapter.structure.FieldByID(ctx, fieldID)
	if err != nil {
		return fmt.Errorf("load converting field %s: %w", fieldID, err)
	}
	sourceOptionNames := optionNamesFromFieldConfig(field.Config)
	operator := conversionOperator(ctx)
	tx, err := adapter.records.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	recordsList, err := adapter.records.RecordsByTable(ctx, tableID, "")
	if err != nil {
		return err
	}
	for _, candidate := range recordsList {
		if isClearedCell(candidate.Cells[fieldID]) {
			continue
		}
		state, err := adapter.records.RecordByIDForUpdate(ctx, tx, candidate.ID)
		if errors.Is(err, base.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if state.SoftDeletedAt != nil {
			continue
		}
		converted, changed := convertFieldValue(
			state.Cells[fieldID],
			field.Type,
			targetType,
			sourceOptionNames,
			targetOptionNames,
		)
		if !changed {
			continue
		}
		next := state.Record
		next.Cells = cloneRecordCells(state.Cells)
		next.Version = state.Version + 1
		setRecordCell(next.Cells, fieldID, converted)
		if err := adapter.records.UpdateRecordTx(ctx, tx, next); err != nil {
			return err
		}
		if err := adapter.appendConversionHistory(
			ctx,
			tx,
			next.ID,
			fieldID,
			state.Cells[fieldID],
			converted,
			operator,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (adapter *FieldConversionAdapter) appendConversionHistory(
	ctx context.Context,
	tx *sql.Tx,
	recordID, fieldID string,
	before, after *commonv1.Value,
	operator string,
) error {
	historyID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate conversion history id: %w", err)
	}
	return adapter.records.AppendHistoryTx(ctx, tx, base.RecordHistory{
		ID:        historyID,
		RecordID:  recordID,
		FieldID:   fieldID,
		Before:    before,
		After:     after,
		Operator:  operator,
		CreatedAt: time.Now().UnixMilli(),
	})
}

// conversionOperator resolves the initiating manager for history attribution.
func conversionOperator(ctx context.Context) string {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil || principal.GetUserId() == "" {
		return "structure"
	}
	return principal.GetUserId()
}

// convertFieldValue applies the PRD 7.1 rule for one value. changed=false 表
// 示值已符合目标语义（无需改写）；被清空的值以 nil 返回。
func convertFieldValue(
	value *commonv1.Value,
	sourceType, targetType string,
	sourceOptionNames, targetOptionNames map[string]string,
) (*commonv1.Value, bool) {
	if value == nil || value.GetKind() == nil || sourceType == targetType {
		return value, false
	}
	if valueMatchesTargetType(value, sourceType, targetType, sourceOptionNames, targetOptionNames) {
		return value, false
	}
	switch {
	case targetType == "FIELD_TYPE_TEXT":
		return convertToTextValue(value, sourceType, sourceOptionNames)
	case sourceType == "FIELD_TYPE_TEXT":
		return convertTextValue(value, targetType, targetOptionNames)
	case sourceType == "FIELD_TYPE_SELECT" && targetType == "FIELD_TYPE_MULTISELECT":
		return &commonv1.Value{Kind: &commonv1.Value_ListValue{
			ListValue: &commonv1.ValueList{ValueList: []*commonv1.Value{value}},
		}}, true
	case sourceType == "FIELD_TYPE_MULTISELECT" && targetType == "FIELD_TYPE_SELECT":
		items := value.GetListValue().GetValueList()
		if len(items) == 0 {
			return nil, true
		}
		return &commonv1.Value{Kind: &commonv1.Value_StringValue{
			StringValue: items[0].GetStringValue(),
		}}, true
	default:
		// 结构层已拒绝非法组合；未知组合保持原值，避免误清数据。
		return value, false
	}
}

// valueMatchesTargetType 判断值已具备目标类型语义。这是重试自愈口径：值迁移
// 成功而结构行更新失败时再次确认转换，已迁移的值不再二次改写或被误清空。
// 选项来源的 id/列表属未迁移值，仍需按选项名渲染。
func valueMatchesTargetType(
	value *commonv1.Value,
	sourceType, targetType string,
	sourceOptionNames, targetOptionNames map[string]string,
) bool {
	switch targetType {
	case "FIELD_TYPE_TEXT":
		if _, ok := value.GetKind().(*commonv1.Value_StringValue); !ok {
			return false
		}
		switch sourceType {
		case "FIELD_TYPE_SELECT", "FIELD_TYPE_MULTISELECT":
			_, knownOptionID := sourceOptionNames[value.GetStringValue()]
			return !knownOptionID
		default:
			return true
		}
	case "FIELD_TYPE_NUMBER":
		switch value.GetKind().(type) {
		case *commonv1.Value_IntValue, *commonv1.Value_DoubleValue:
			return true
		}
		return false
	case "FIELD_TYPE_DATE":
		switch value.GetKind().(type) {
		case *commonv1.Value_DateValueMs, *commonv1.Value_IntValue:
			return true
		}
		return false
	case "FIELD_TYPE_SELECT":
		if _, ok := value.GetKind().(*commonv1.Value_StringValue); !ok {
			return false
		}
		_, known := targetOptionNames[value.GetStringValue()]
		return known
	case "FIELD_TYPE_MULTISELECT":
		items, ok := value.GetKind().(*commonv1.Value_ListValue)
		if !ok {
			return false
		}
		for _, item := range items.ListValue.GetValueList() {
			if _, known := targetOptionNames[item.GetStringValue()]; !known {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// convertToTextValue 按 PRD 7.1「任意 → 多行文本」渲染显示值。
func convertToTextValue(
	value *commonv1.Value,
	sourceType string,
	sourceOptionNames map[string]string,
) (*commonv1.Value, bool) {
	switch sourceType {
	case "FIELD_TYPE_NUMBER":
		switch kind := value.GetKind().(type) {
		case *commonv1.Value_IntValue:
			return textValue(strconv.FormatInt(kind.IntValue, 10)), true
		case *commonv1.Value_DoubleValue:
			return textValue(strconv.FormatFloat(kind.DoubleValue, 'f', -1, 64)), true
		default:
			return value, false
		}
	case "FIELD_TYPE_DATE":
		var millis int64
		switch kind := value.GetKind().(type) {
		case *commonv1.Value_IntValue:
			millis = kind.IntValue
		case *commonv1.Value_DateValueMs:
			millis = kind.DateValueMs
		default:
			return value, false
		}
		return textValue(time.UnixMilli(millis).In(convertDisplayZone).Format("2006-01-02")), true
	case "FIELD_TYPE_SELECT":
		name, exists := sourceOptionNames[value.GetStringValue()]
		if !exists {
			return nil, true
		}
		return textValue(name), true
	case "FIELD_TYPE_MULTISELECT":
		names := make([]string, 0, len(value.GetListValue().GetValueList()))
		for _, item := range value.GetListValue().GetValueList() {
			if name, exists := sourceOptionNames[item.GetStringValue()]; exists {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return nil, true
		}
		return textValue(strings.Join(names, ",")), true
	case "FIELD_TYPE_PERSON", "FIELD_TYPE_ATTACHMENT":
		// PRD 7.1 要求取姓名/文件名；用户与附件档案解析属 iam/worker 域，
		// 当前以 id 串接兜底（已知偏差，待专用 seam 后对齐显示值）。
		return idListToTextValue(value), true
	default:
		return value, false
	}
}

func idListToTextValue(value *commonv1.Value) *commonv1.Value {
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_StringValue:
		return textValue(kind.StringValue)
	case *commonv1.Value_ListValue:
		ids := make([]string, 0, len(kind.ListValue.GetValueList()))
		for _, item := range kind.ListValue.GetValueList() {
			if text := item.GetStringValue(); text != "" {
				ids = append(ids, text)
			}
		}
		return textValue(strings.Join(ids, ","))
	default:
		return value
	}
}

// convertTextValue 按 PRD 7.1 处理文本来源的转换；解析/匹配失败置空。
func convertTextValue(
	value *commonv1.Value,
	targetType string,
	targetOptionNames map[string]string,
) (*commonv1.Value, bool) {
	raw := strings.TrimSpace(value.GetStringValue())
	switch targetType {
	case "FIELD_TYPE_NUMBER":
		return parseNumberText(value.GetStringValue())
	case "FIELD_TYPE_DATE":
		return parseDateText(value.GetStringValue())
	case "FIELD_TYPE_SELECT":
		optionID, exists := matchOptionName(raw, targetOptionNames)
		if !exists {
			return nil, true
		}
		return textValue(optionID), true
	case "FIELD_TYPE_MULTISELECT":
		optionID, exists := matchOptionName(raw, targetOptionNames)
		if !exists {
			return nil, true
		}
		return &commonv1.Value{Kind: &commonv1.Value_ListValue{
			ListValue: &commonv1.ValueList{ValueList: []*commonv1.Value{textValue(optionID)}},
		}}, true
	default:
		return value, false
	}
}

// parseNumberText 去千分位与货币符号后解析；百分比按数值/100；失败置空。
func parseNumberText(raw string) (*commonv1.Value, bool) {
	text := convertNumberStripper.Replace(strings.TrimSpace(raw))
	percent := strings.HasSuffix(text, "%")
	text = strings.TrimSpace(strings.TrimSuffix(text, "%"))
	if text == "" {
		return nil, true
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil, true
	}
	if percent {
		parsed /= 100
	}
	if parsed == math.Trunc(parsed) && math.Abs(parsed) < 1e15 {
		return &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: int64(parsed)}}, true
	}
	return &commonv1.Value{Kind: &commonv1.Value_DoubleValue{DoubleValue: parsed}}, true
}

// parseDateText 按常见格式解析；失败置空（PRD 7.1）。
func parseDateText(raw string) (*commonv1.Value, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, true
	}
	for _, layout := range convertDateLayouts {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return &commonv1.Value{
				Kind: &commonv1.Value_DateValueMs{DateValueMs: parsed.UnixMilli()},
			}, true
		}
	}
	return nil, true
}

// matchOptionName 按选项名精确匹配目标选项（不自动新建，PRD 7.1）。
func matchOptionName(name string, targetOptionNames map[string]string) (string, bool) {
	for optionID, optionName := range targetOptionNames {
		if optionName == name {
			return optionID, true
		}
	}
	return "", false
}

func textValue(text string) *commonv1.Value {
	return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: text}}
}

// optionNamesFromFieldConfig 从字段配置 JSON 解码选项 id→名 映射。
func optionNamesFromFieldConfig(config []byte) map[string]string {
	decoded, err := base.DecodeStructureConfig(config)
	if err != nil {
		return map[string]string{}
	}
	return optionNamesFromConfigMap(decoded)
}

func optionNamesFromConfigMap(config map[string]any) map[string]string {
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
