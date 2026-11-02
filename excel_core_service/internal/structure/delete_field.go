package structure

import (
	"context"
	"regexp"
	"strings"

	structurev1 "excel/api/structure/v1"
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
	if _, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		field.TableID,
	); err != nil {
		return nil, mapStructureError(err)
	}
	if field.IsPrimary {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if service.impact == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
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
	if err := service.repository.DeleteField(ctx, field.ID); err != nil {
		return nil, mapStructureError(err)
	}
	return response, nil
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
