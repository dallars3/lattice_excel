package structure

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
	"excel_core_service/internal/calc"
)

var formulaNameReferencePattern = regexp.MustCompile(`\{([^{}]+)\}`)

// DeleteField previews impact, then deletes only when confirm is true.
func (service *Service) DeleteField(
	ctx context.Context,
	request *structurev1.DeleteFieldRequest,
) (*structurev1.DeleteFieldResponse, error) {
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
	if field.IsPrimary {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if service.impact == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	recordCount, err := service.impact.AffectedRecords(ctx, field.TableID, field.ID)
	if err != nil {
		return nil, err
	}
	fields, err := service.schema.Fields(ctx)
	if err != nil {
		return nil, err
	}
	formulas, lookups := dependencyCounts(fields, field.TableID, field.ID)
	response := &structurev1.DeleteFieldResponse{
		RecordCount:       int32(recordCount),
		DependentFormulas: int32(formulas),
		DependentLookups:  int32(lookups),
	}
	if !request.GetConfirm() {
		return response, nil
	}
	if err := service.deleteFieldCascade(ctx, table, field, principal.GetUserId()); err != nil {
		return nil, err
	}
	return response, nil
}

// deleteFieldCascade removes the field and rewrites every view config that
// still references it（工单 06：筛选/隐藏/冻结/排序/分组/宽度六类槽位级联清理，
// 视图改写与字段删除同事务生效；缺陷 #18：删除成功与 field_deleted 广播事件
// 同事务落 outbox）。
func (service *Service) deleteFieldCascade(
	ctx context.Context,
	table base.StructureTable,
	field base.StructureField,
	operator string,
) error {
	views, err := service.repository.ViewsByTable(ctx, field.TableID)
	if err != nil {
		return mapStructureError(err)
	}
	cleaned := make([]base.StructureView, 0, len(views))
	for _, view := range views {
		config, err := decodeViewConfig(view.Config)
		if err != nil {
			return err
		}
		if !stripFieldFromViewConfig(config, field.ID) {
			continue
		}
		payload, err := encodeViewConfig(config)
		if err != nil {
			return err
		}
		view.Config = payload
		cleaned = append(cleaned, view)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.DeleteFieldWithViewCleanupTx(ctx, tx, field.ID, cleaned); err != nil {
		return mapStructureError(err)
	}
	// N5 票 03：物化列清理与字段删除同事务生效（与转出公式/引用同口径），
	// 删除成功不留孤儿投影。
	if err := service.cleanupFieldMaterializationTx(ctx, tx, field.ID); err != nil {
		return err
	}
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		field.ID,
		structureChangeFieldDeleted,
		operator,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit DeleteField: %w", err)
	}
	return nil
}

// stripFieldFromViewConfig removes every reference to the deleted field from
// one view config（六类槽位：filter/hidden/frozen/sort/group/width）；
// 返回是否有改动。
func stripFieldFromViewConfig(
	config *structurev1.ViewConfig,
	fieldID string,
) bool {
	if config == nil {
		return false
	}
	changed := false
	keptFilters := make([]*structurev1.ViewFilter, 0, len(config.GetFilterList()))
	for _, filter := range config.GetFilterList() {
		if filter.GetFieldId() == fieldID {
			changed = true
			continue
		}
		keptFilters = append(keptFilters, filter)
	}
	config.FilterList = keptFilters

	keptHidden := make([]string, 0, len(config.GetHiddenFieldList()))
	for _, hidden := range config.GetHiddenFieldList() {
		if hidden == fieldID {
			changed = true
			continue
		}
		keptHidden = append(keptHidden, hidden)
	}
	config.HiddenFieldList = keptHidden

	keptFrozen := make([]string, 0, len(config.GetFrozenFieldList()))
	for _, frozen := range config.GetFrozenFieldList() {
		if frozen == fieldID {
			changed = true
			continue
		}
		keptFrozen = append(keptFrozen, frozen)
	}
	config.FrozenFieldList = keptFrozen

	keptSorts := make([]*structurev1.ViewSort, 0, len(config.GetSortList()))
	for _, sort := range config.GetSortList() {
		if sort.GetFieldId() == fieldID {
			changed = true
			continue
		}
		keptSorts = append(keptSorts, sort)
	}
	config.SortList = keptSorts

	keptGroups := make([]*structurev1.ViewGroup, 0, len(config.GetGroupByList()))
	for _, group := range config.GetGroupByList() {
		if group.GetFieldId() == fieldID {
			changed = true
			continue
		}
		keptGroups = append(keptGroups, group)
	}
	config.GroupByList = keptGroups

	if _, exists := config.GetFieldWidthMap()[fieldID]; exists {
		delete(config.FieldWidthMap, fieldID)
		changed = true
	}
	return changed
}

type calcFieldKey struct {
	tableID string
	fieldID string
}

func dependencyCounts(
	fields []calc.FieldDefinition,
	targetTableID, targetFieldID string,
) (int, int) {
	nodes := make(map[calcFieldKey]calc.FieldDefinition, len(fields))
	byTableName := make(map[string]map[string]calcFieldKey)
	for _, field := range fields {
		key := calcFieldKey{tableID: field.TableID, fieldID: field.ID}
		nodes[key] = field
		if byTableName[field.TableID] == nil {
			byTableName[field.TableID] = make(map[string]calcFieldKey)
		}
		byTableName[field.TableID][field.Name] = key
	}
	reverseEdges := make(map[calcFieldKey]map[calcFieldKey]struct{}, len(fields))
	addEdge := func(source, target calcFieldKey) {
		if reverseEdges[target] == nil {
			reverseEdges[target] = make(map[calcFieldKey]struct{})
		}
		reverseEdges[target][source] = struct{}{}
	}
	for _, field := range fields {
		source := calcFieldKey{tableID: field.TableID, fieldID: field.ID}
		if field.Type == "FIELD_TYPE_FORMULA" {
			for _, match := range formulaNameReferencePattern.FindAllStringSubmatch(field.Formula, -1) {
				name := strings.TrimSpace(match[1])
				if target, ok := byTableName[field.TableID][name]; ok {
					addEdge(source, target)
				}
			}
		}
		if field.Relation != nil {
			addEdge(source, calcFieldKey{
				tableID: field.Relation.TableID,
				fieldID: field.Relation.FieldID,
			})
		}
		if field.Lookup != nil {
			relationKey := calcFieldKey{
				tableID: field.TableID,
				fieldID: field.Lookup.RelationFieldID,
			}
			addEdge(source, relationKey)
			if relation, ok := nodes[relationKey]; ok && relation.Relation != nil {
				addEdge(source, calcFieldKey{
					tableID: relation.Relation.TableID,
					fieldID: field.Lookup.TargetFieldID,
				})
			}
		}
	}

	target := calcFieldKey{tableID: targetTableID, fieldID: targetFieldID}
	seen := map[calcFieldKey]struct{}{target: {}}
	queue := []calcFieldKey{target}
	formulas := 0
	lookups := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for dependent := range reverseEdges[current] {
			if _, exists := seen[dependent]; exists {
				continue
			}
			seen[dependent] = struct{}{}
			queue = append(queue, dependent)
			switch nodes[dependent].Type {
			case "FIELD_TYPE_FORMULA":
				formulas++
			case "FIELD_TYPE_LOOKUP":
				lookups++
			}
		}
	}
	return formulas, lookups
}
