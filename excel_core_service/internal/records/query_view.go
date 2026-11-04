// records/query_view.go：视图筛选、排序、分组与游标分页。
package records

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

type viewCursor struct {
	Group    any    `json:"group"`
	SortKeys []any  `json:"sort_keys"`
	RecordID string `json:"record_id"`
}

// QueryView applies one persisted view to active records.
func (service *Service) QueryView(
	ctx context.Context,
	request *recordsv1.QueryViewRequest,
) (*recordsv1.QueryViewResponse, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("records repository is not configured")
	}
	if request == nil ||
		strings.TrimSpace(request.GetBaseId()) == "" ||
		strings.TrimSpace(request.GetTableId()) == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	scope, err := rowScopeFromContext(ctx)
	if err != nil {
		return nil, err
	}
	table, err := service.tableForBase(ctx, request.GetBaseId(), request.GetTableId())
	if err != nil {
		return nil, err
	}
	fields, err := service.structure.Fields(ctx, table.GetId())
	if err != nil {
		return nil, mapRecordError(err)
	}
	config := &structurev1.ViewConfig{}
	if request.GetViewId() != "" {
		view, viewConfig, err := service.structure.View(ctx, request.GetViewId())
		if err != nil {
			return nil, mapRecordError(err)
		}
		if view == nil || view.GetTableId() != table.GetId() {
			return nil, deal.RPCError(deal.CodeInvalidArgument)
		}
		if viewConfig != nil {
			config = viewConfig
		}
	}
	if len(config.GetFilterList()) > 5 ||
		len(config.GetSortList()) > 3 ||
		len(config.GetGroupByList()) > 1 {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	limit := normalizePageLimit(request.GetLimit())
	createdBy := ""
	if scope == rowScopeOwn {
		createdBy = principal.GetUserId()
	}
	records, err := service.repository.RecordsByTable(ctx, table.GetId(), createdBy)
	if err != nil {
		return nil, err
	}
	fieldMap := fieldByID(fields)
	for index := range records {
		if err := service.decorateRecord(
			ctx,
			principal,
			request.GetBaseId(),
			&records[index],
			fieldMap,
		); err != nil {
			return nil, err
		}
	}
	filtered := records[:0]
	for _, record := range records {
		matches, err := matchesFilters(record, fieldMap, config.GetFilterList(), service.now())
		if err != nil {
			return nil, err
		}
		if matches {
			filtered = append(filtered, record)
		}
	}
	records = filtered
	sort.SliceStable(records, func(left, right int) bool {
		return compareRecords(
			records[left],
			records[right],
			fieldMap,
			config,
			service.location,
		) < 0
	})

	start := 0
	if request.GetCursor() != "" {
		cursor, err := decodeViewCursor(request.GetCursor())
		if err != nil {
			return nil, err
		}
		start, err = cursorPosition(records, cursor, fieldMap, config, service.location)
		if err != nil {
			return nil, err
		}
	}
	end := start + limit
	if end > len(records) {
		end = len(records)
	}
	page := records[start:end]
	response := &recordsv1.QueryViewResponse{
		RecordList: make([]*recordsv1.Record, 0, len(page)),
		HasMore:    end < len(records),
	}
	hidden := make(map[string]struct{}, len(config.GetHiddenFieldList()))
	for _, fieldID := range config.GetHiddenFieldList() {
		hidden[fieldID] = struct{}{}
	}
	for _, record := range page {
		projected := recordToProto(record)
		for fieldID := range hidden {
			delete(projected.CellsMap, fieldID)
		}
		response.RecordList = append(response.RecordList, projected)
	}
	// 每页一次批量聚合页内记录集的未解决评论数；计数为 0 的记录不出现在 map。
	if service.comments != nil && len(page) > 0 {
		pageRecordIDs := make([]string, len(page))
		for index, record := range page {
			pageRecordIDs[index] = record.ID
		}
		counts, err := service.comments.UnsolvedCounts(ctx, pageRecordIDs)
		if err != nil {
			return nil, err
		}
		if len(counts) > 0 {
			response.UnsolvedCommentCountMap = counts
		}
	}
	if response.HasMore && len(page) > 0 {
		response.NextCursor, err = encodeViewCursor(
			recordCursorKey(page[len(page)-1], fieldMap, config, service.location),
		)
		if err != nil {
			return nil, err
		}
	}
	return response, nil
}

func normalizePageLimit(limit int32) int {
	if limit <= 0 {
		return defaultPageLimit
	}
	if limit > maxPageLimit {
		return maxPageLimit
	}
	return int(limit)
}

func (service *Service) decorateRecord(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
	record *base.Record,
	fields map[string]*structurev1.Field,
) error {
	if record == nil {
		return nil
	}
	for fieldID, field := range fields {
		if field.GetType() != structurev1.FieldType_FIELD_TYPE_FORMULA {
			continue
		}
		if service.formulas == nil {
			delete(record.Cells, fieldID)
			continue
		}
		value, ok, err := service.formulas.Value(ctx, record.ID, fieldID)
		if err != nil {
			return err
		}
		if ok {
			record.Cells[fieldID] = value
		} else {
			delete(record.Cells, fieldID)
		}
	}
	for fieldID, field := range fields {
		switch field.GetType() {
		case structurev1.FieldType_FIELD_TYPE_RELATION:
			if err := service.filterRelationCell(
				ctx,
				principal,
				baseID,
				record,
				fieldID,
				field,
			); err != nil {
				return err
			}
		case structurev1.FieldType_FIELD_TYPE_LOOKUP:
			if err := service.materializeLookupCell(
				ctx,
				principal,
				baseID,
				record,
				fieldID,
				field,
				fields,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (service *Service) filterRelationCell(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
	record *base.Record,
	fieldID string,
	field *structurev1.Field,
) error {
	value := record.Cells[fieldID]
	recordIDs, allowMultiple := relationRecordIDs(value, field)
	if len(recordIDs) == 0 {
		delete(record.Cells, fieldID)
		return nil
	}
	targetTableID, err := fieldConfigString(field, "target_table_id")
	if err != nil {
		return err
	}
	visible := make(map[string]base.Record, len(recordIDs))
	targetRecords, err := service.visibleTargetRecords(
		ctx,
		principal,
		baseID,
		targetTableID,
		recordIDs,
	)
	if err != nil {
		return err
	}
	for _, target := range targetRecords {
		visible[target.ID] = target
	}
	kept := make([]string, 0, len(recordIDs))
	for _, recordID := range recordIDs {
		if _, ok := visible[recordID]; ok {
			kept = append(kept, recordID)
		}
	}
	if len(kept) == 0 {
		delete(record.Cells, fieldID)
		return nil
	}
	if !allowMultiple {
		record.Cells[fieldID] = stringCellValue(kept[0])
		return nil
	}
	values := make([]*commonv1.Value, 0, len(kept))
	for _, recordID := range kept {
		values = append(values, stringCellValue(recordID))
	}
	record.Cells[fieldID] = listCellValue(values...)
	return nil
}

func (service *Service) materializeLookupCell(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID string,
	record *base.Record,
	fieldID string,
	lookup *structurev1.Field,
	fields map[string]*structurev1.Field,
) error {
	relationFieldID, err := fieldConfigString(lookup, "relation_field_id")
	if err != nil {
		return err
	}
	targetFieldID, err := fieldConfigString(lookup, "target_field_id")
	if err != nil {
		return err
	}
	relationField, ok := fields[relationFieldID]
	if !ok || relationField.GetType() != structurev1.FieldType_FIELD_TYPE_RELATION {
		delete(record.Cells, fieldID)
		return nil
	}
	recordIDs, allowMultiple := relationRecordIDs(record.Cells[relationFieldID], relationField)
	if len(recordIDs) == 0 {
		delete(record.Cells, fieldID)
		return nil
	}
	targetTableID, err := fieldConfigString(relationField, "target_table_id")
	if err != nil {
		return err
	}
	targetRecords, err := service.visibleTargetRecords(
		ctx,
		principal,
		baseID,
		targetTableID,
		recordIDs,
	)
	if err != nil {
		return err
	}
	byID := make(map[string]base.Record, len(targetRecords))
	for _, target := range targetRecords {
		byID[target.ID] = target
	}
	values := make([]*commonv1.Value, 0, len(recordIDs))
	for _, recordID := range recordIDs {
		target, ok := byID[recordID]
		if !ok {
			continue
		}
		targetField, ok := fields[targetFieldID]
		if ok && targetField.GetType() == structurev1.FieldType_FIELD_TYPE_FORMULA {
			if service.formulas == nil {
				delete(target.Cells, targetFieldID)
			} else {
				value, materialized, err := service.formulas.Value(ctx, target.ID, targetFieldID)
				if err != nil {
					return err
				}
				if materialized {
					target.Cells[targetFieldID] = value
				} else {
					delete(target.Cells, targetFieldID)
				}
			}
		}
		if value := target.Cells[targetFieldID]; value != nil {
			if _, isList := value.GetKind().(*commonv1.Value_ListValue); isList {
				values = append(values, value.GetListValue().GetValueList()...)
			} else {
				values = append(values, value)
			}
		}
	}
	if len(values) == 0 {
		delete(record.Cells, fieldID)
		return nil
	}
	if allowMultiple || len(values) > 1 {
		record.Cells[fieldID] = listCellValue(values...)
	} else {
		record.Cells[fieldID] = values[0]
	}
	return nil
}

func (service *Service) visibleTargetRecords(
	ctx context.Context,
	principal *commonv1.Principal,
	baseID, tableID string,
	recordIDs []string,
) ([]base.Record, error) {
	if service.scopes == nil {
		return nil, errors.New("records row scope provider is not configured")
	}
	scope, err := service.scopes.RowScope(ctx, principal, baseID)
	if err != nil {
		return nil, err
	}
	scope, err = normalizeRowScope(scope)
	if err != nil {
		return nil, err
	}
	createdBy := ""
	if scope == rowScopeOwn {
		createdBy = principal.GetUserId()
	}
	return service.repository.RecordsByIDs(
		ctx,
		tableID,
		recordIDs,
		createdBy,
	)
}

func relationRecordIDs(
	value *commonv1.Value,
	field *structurev1.Field,
) ([]string, bool) {
	allowMultiple, _ := fieldConfigBool(field, "allow_multiple")
	if value == nil {
		return nil, allowMultiple
	}
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_StringValue:
		if kind.StringValue == "" {
			return nil, allowMultiple
		}
		return []string{kind.StringValue}, allowMultiple
	case *commonv1.Value_ListValue:
		recordIDs := make([]string, 0, len(kind.ListValue.GetValueList()))
		for _, item := range kind.ListValue.GetValueList() {
			if item.GetStringValue() != "" {
				recordIDs = append(recordIDs, item.GetStringValue())
			}
		}
		return recordIDs, allowMultiple
	default:
		return nil, allowMultiple
	}
}

func fieldConfigString(field *structurev1.Field, key string) (string, error) {
	if field == nil || field.GetConfigMap() == nil {
		return "", errors.New("field config is required")
	}
	value, ok := field.GetConfigMap().GetFields()[key]
	if !ok || strings.TrimSpace(value.GetStringValue()) == "" {
		return "", fmt.Errorf("field %s config %s is required", field.GetId(), key)
	}
	return value.GetStringValue(), nil
}

func fieldConfigBool(field *structurev1.Field, key string) (bool, bool) {
	if field == nil || field.GetConfigMap() == nil {
		return false, false
	}
	value, ok := field.GetConfigMap().GetFields()[key]
	if !ok {
		return false, false
	}
	return value.GetBoolValue(), true
}

func matchesFilters(
	record base.Record,
	fields map[string]*structurev1.Field,
	filters []*structurev1.ViewFilter,
	now time.Time,
) (bool, error) {
	for _, filter := range filters {
		if filter == nil {
			return false, deal.RPCError(deal.CodeInvalidArgument)
		}
		field, ok := fields[filter.GetFieldId()]
		if !ok {
			return false, deal.RPCError(deal.CodeInvalidArgument)
		}
		value, exists := record.Cells[filter.GetFieldId()]
		if !matchesFilter(value, exists, field, filter.GetOp(), filter.GetValue(), now) {
			return false, nil
		}
	}
	return true, nil
}

func matchesFilter(
	value *commonv1.Value,
	exists bool,
	field *structurev1.Field,
	op structurev1.ViewFilterOp,
	expected *commonv1.Value,
	now time.Time,
) bool {
	empty := isEmptyCell(value, exists)
	switch op {
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_IS_EMPTY:
		return empty
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_NOT_EMPTY:
		return !empty
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_NONE:
		return empty
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_HAS:
		return !empty
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_TODAY:
		return isToday(value, now)
	}
	if empty || expected == nil {
		return false
	}
	switch op {
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_CONTAINS:
		return cellContains(value, expected)
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_NOT_CONTAINS:
		return !cellContains(value, expected)
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_EQ:
		return compareCells(value, expected, field.GetType()) == 0
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_NE:
		return compareCells(value, expected, field.GetType()) != 0
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_GT:
		return compareCells(value, expected, field.GetType()) > 0
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_LT:
		return compareCells(value, expected, field.GetType()) < 0
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_BEFORE:
		return compareCells(value, expected, field.GetType()) < 0
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_AFTER:
		return compareCells(value, expected, field.GetType()) > 0
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_BETWEEN:
		bounds := expected.GetListValue().GetValueList()
		return len(bounds) >= 2 &&
			compareCells(value, bounds[0], field.GetType()) >= 0 &&
			compareCells(value, bounds[1], field.GetType()) <= 0
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_IN:
		return cellIn(value, expected)
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_NOT_IN:
		return !cellIn(value, expected)
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_CONTAINS_ANY:
		return listIntersects(value, expected, false)
	case structurev1.ViewFilterOp_VIEW_FILTER_OP_CONTAINS_ALL:
		return listIntersects(value, expected, true)
	default:
		return false
	}
}

func isEmptyCell(value *commonv1.Value, exists bool) bool {
	if !exists || value == nil {
		return true
	}
	switch kind := value.GetKind().(type) {
	case nil:
		return true
	case *commonv1.Value_StringValue:
		return strings.TrimSpace(kind.StringValue) == ""
	case *commonv1.Value_ListValue:
		return len(kind.ListValue.GetValueList()) == 0
	default:
		return false
	}
}

func cellContains(value, expected *commonv1.Value) bool {
	if text, ok := stringValue(value); ok {
		return strings.Contains(
			strings.ToLower(text),
			strings.ToLower(expected.GetStringValue()),
		)
	}
	if list, ok := listValues(value); ok {
		for _, item := range list {
			if compareCells(item, expected, structurev1.FieldType_FIELD_TYPE_TEXT) == 0 {
				return true
			}
		}
	}
	return false
}

func cellIn(value, expected *commonv1.Value) bool {
	options, ok := listValues(expected)
	if !ok {
		options = []*commonv1.Value{expected}
	}
	values, ok := listValues(value)
	if !ok {
		values = []*commonv1.Value{value}
	}
	for _, candidate := range values {
		for _, option := range options {
			if compareCells(candidate, option, structurev1.FieldType_FIELD_TYPE_TEXT) == 0 {
				return true
			}
		}
	}
	return false
}

func listIntersects(value, expected *commonv1.Value, requireAll bool) bool {
	values, ok := listValues(value)
	if !ok {
		values = []*commonv1.Value{value}
	}
	options, ok := listValues(expected)
	if !ok {
		options = []*commonv1.Value{expected}
	}
	matched := 0
	for _, option := range options {
		for _, candidate := range values {
			if compareCells(candidate, option, structurev1.FieldType_FIELD_TYPE_TEXT) == 0 {
				matched++
				break
			}
		}
	}
	if requireAll {
		return matched == len(options)
	}
	return matched > 0
}

func isToday(value *commonv1.Value, now time.Time) bool {
	millis, ok := dateValue(value)
	if !ok {
		return false
	}
	valueTime := time.UnixMilli(millis).In(now.Location())
	return valueTime.Year() == now.Year() &&
		valueTime.YearDay() == now.YearDay()
}

func compareRecords(
	left, right base.Record,
	fields map[string]*structurev1.Field,
	config *structurev1.ViewConfig,
	location *time.Location,
) int {
	if groups := config.GetGroupByList(); len(groups) == 1 {
		field := fields[groups[0].GetFieldId()]
		if comparison := compareGroupCells(
			left.Cells[groups[0].GetFieldId()],
			right.Cells[groups[0].GetFieldId()],
			field,
			groups[0].GetGranularity(),
			location,
		); comparison != 0 {
			return comparison
		}
	}
	for _, sortSpec := range config.GetSortList() {
		field := fields[sortSpec.GetFieldId()]
		comparison := compareNullableCells(
			left.Cells[sortSpec.GetFieldId()],
			right.Cells[sortSpec.GetFieldId()],
			field,
			sortSpec.GetDir() == structurev1.SortDir_SORT_DIR_ASC,
		)
		if comparison != 0 {
			return comparison
		}
	}
	if left.ID < right.ID {
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	return 0
}

func compareNullableCells(
	left, right *commonv1.Value,
	field *structurev1.Field,
	ascending bool,
) int {
	leftEmpty := isEmptyCell(left, left != nil)
	rightEmpty := isEmptyCell(right, right != nil)
	if leftEmpty || rightEmpty {
		switch {
		case leftEmpty && rightEmpty:
			return 0
		case leftEmpty:
			return 1
		default:
			return -1
		}
	}
	comparison := compareCells(left, right, field.GetType())
	if !ascending {
		comparison = -comparison
	}
	return comparison
}

func compareGroupCells(
	left, right *commonv1.Value,
	field *structurev1.Field,
	granularity structurev1.Granularity,
	location *time.Location,
) int {
	leftEmpty := isEmptyCell(left, left != nil)
	rightEmpty := isEmptyCell(right, right != nil)
	if leftEmpty || rightEmpty {
		switch {
		case leftEmpty && rightEmpty:
			return 0
		case leftEmpty:
			return 1
		default:
			return -1
		}
	}
	if field.GetType() == structurev1.FieldType_FIELD_TYPE_DATE {
		leftMillis, leftOK := dateValue(left)
		rightMillis, rightOK := dateValue(right)
		if leftOK && rightOK {
			leftKey := dateGroupKey(leftMillis, granularity, location)
			rightKey := dateGroupKey(rightMillis, granularity, location)
			switch {
			case leftKey < rightKey:
				return -1
			case leftKey > rightKey:
				return 1
			default:
				return 0
			}
		}
	}
	return compareCells(left, right, field.GetType())
}

func compareCells(
	left, right *commonv1.Value,
	fieldType structurev1.FieldType,
) int {
	if leftNumber, ok := numberValue(left); ok {
		if rightNumber, ok := numberValue(right); ok {
			switch {
			case leftNumber < rightNumber:
				return -1
			case leftNumber > rightNumber:
				return 1
			default:
				return 0
			}
		}
	}
	if fieldType == structurev1.FieldType_FIELD_TYPE_DATE {
		leftDate, leftOK := dateValue(left)
		rightDate, rightOK := dateValue(right)
		if leftOK && rightOK {
			switch {
			case leftDate < rightDate:
				return -1
			case leftDate > rightDate:
				return 1
			default:
				return 0
			}
		}
	}
	leftText := canonicalCell(left)
	rightText := canonicalCell(right)
	return strings.Compare(leftText, rightText)
}

func canonicalCell(value *commonv1.Value) string {
	if value == nil {
		return ""
	}
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_StringValue:
		return kind.StringValue
	case *commonv1.Value_IntValue:
		return fmt.Sprintf("%020d", kind.IntValue)
	case *commonv1.Value_DoubleValue:
		return fmt.Sprintf("%020.8f", kind.DoubleValue)
	case *commonv1.Value_BoolValue:
		return fmt.Sprintf("%t", kind.BoolValue)
	case *commonv1.Value_DateValueMs:
		return fmt.Sprintf("%020d", kind.DateValueMs)
	case *commonv1.Value_ListValue:
		parts := make([]string, 0, len(kind.ListValue.GetValueList()))
		for _, item := range kind.ListValue.GetValueList() {
			parts = append(parts, canonicalCell(item))
		}
		return strings.Join(parts, "\x00")
	default:
		return ""
	}
}

func numberValue(value *commonv1.Value) (float64, bool) {
	if value == nil {
		return 0, false
	}
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_IntValue:
		return float64(kind.IntValue), true
	case *commonv1.Value_DoubleValue:
		return kind.DoubleValue, true
	default:
		return 0, false
	}
}

func dateValue(value *commonv1.Value) (int64, bool) {
	if value == nil {
		return 0, false
	}
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_DateValueMs:
		return kind.DateValueMs, true
	case *commonv1.Value_IntValue:
		return kind.IntValue, true
	default:
		return 0, false
	}
}

func stringValue(value *commonv1.Value) (string, bool) {
	if value == nil {
		return "", false
	}
	kind, ok := value.GetKind().(*commonv1.Value_StringValue)
	if !ok {
		return "", false
	}
	return kind.StringValue, true
}

func listValues(value *commonv1.Value) ([]*commonv1.Value, bool) {
	if value == nil {
		return nil, false
	}
	kind, ok := value.GetKind().(*commonv1.Value_ListValue)
	if !ok {
		return nil, false
	}
	return kind.ListValue.GetValueList(), true
}

func stringCellValue(value string) *commonv1.Value {
	return &commonv1.Value{
		Kind: &commonv1.Value_StringValue{StringValue: value},
	}
}

func listCellValue(values ...*commonv1.Value) *commonv1.Value {
	return &commonv1.Value{
		Kind: &commonv1.Value_ListValue{
			ListValue: &commonv1.ValueList{ValueList: values},
		},
	}
}

func recordCursorKey(
	record base.Record,
	fields map[string]*structurev1.Field,
	config *structurev1.ViewConfig,
	location *time.Location,
) viewCursor {
	cursor := viewCursor{
		RecordID: record.ID,
		SortKeys: make([]any, 0, len(config.GetSortList())),
	}
	if groups := config.GetGroupByList(); len(groups) == 1 {
		cursor.Group = cursorCellValue(
			record.Cells[groups[0].GetFieldId()],
			fields[groups[0].GetFieldId()],
			groups[0].GetGranularity(),
			location,
		)
	}
	for _, sortSpec := range config.GetSortList() {
		cursor.SortKeys = append(
			cursor.SortKeys,
			cursorCellValue(
				record.Cells[sortSpec.GetFieldId()],
				fields[sortSpec.GetFieldId()],
				structurev1.Granularity_GRANULARITY_UNSPECIFIED,
				location,
			),
		)
	}
	return cursor
}

func cursorCellValue(
	value *commonv1.Value,
	field *structurev1.Field,
	granularity structurev1.Granularity,
	location *time.Location,
) any {
	if isEmptyCell(value, value != nil) {
		return nil
	}
	if field.GetType() == structurev1.FieldType_FIELD_TYPE_DATE {
		millis, _ := dateValue(value)
		return dateGroupKey(millis, granularity, location)
	}
	if number, ok := numberValue(value); ok {
		return number
	}
	if text, ok := stringValue(value); ok {
		return text
	}
	if values, ok := listValues(value); ok {
		result := make([]any, 0, len(values))
		for _, item := range values {
			result = append(result, cursorCellValue(item, field, granularity, location))
		}
		return result
	}
	if kind, ok := value.GetKind().(*commonv1.Value_BoolValue); ok {
		return kind.BoolValue
	}
	return canonicalCell(value)
}

func dateGroupKey(millis int64, granularity structurev1.Granularity, location *time.Location) int64 {
	value := time.UnixMilli(millis).In(location)
	switch granularity {
	case structurev1.Granularity_GRANULARITY_MONTH:
		return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, location).UnixMilli()
	case structurev1.Granularity_GRANULARITY_WEEK:
		offset := (int(value.Weekday()) + 6) % 7
		start := value.AddDate(0, 0, -offset)
		return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location).UnixMilli()
	case structurev1.Granularity_GRANULARITY_DAY:
		return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location).UnixMilli()
	default:
		return millis
	}
}

func cursorPosition(
	records []base.Record,
	cursor viewCursor,
	fields map[string]*structurev1.Field,
	config *structurev1.ViewConfig,
	location *time.Location,
) (int, error) {
	for index, record := range records {
		if record.ID == cursor.RecordID {
			return index + 1, nil
		}
	}
	return 0, statusInvalidCursor()
}

func statusInvalidCursor() error {
	return statusErrorInvalidArgument("cursor does not match the current view result")
}

func statusErrorInvalidArgument(message string) error {
	return dataError("%s", message)
}

func encodeViewCursor(cursor viewCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeViewCursor(value string) (viewCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return viewCursor{}, statusInvalidCursor()
	}
	var cursor viewCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.RecordID == "" {
		return viewCursor{}, statusInvalidCursor()
	}
	return cursor, nil
}
