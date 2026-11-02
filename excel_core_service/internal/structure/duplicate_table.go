package structure

import (
	"context"
	"fmt"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

// DuplicateTable deeply copies structure, options and views, then delegates
// record-data copying to the optional records seam.
func (service *Service) DuplicateTable(
	ctx context.Context,
	request *structurev1.DuplicateTableRequest,
) (*structurev1.DuplicateTableResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	source, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		request.GetTableId(),
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	count, err := service.repository.CountTables(ctx, source.EnterpriseID, source.BaseID)
	if err != nil {
		return nil, err
	}
	if count >= maxTablesPerBase {
		return nil, rpcError(5001)
	}
	tableID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	name, err := service.uniqueTableCopyName(ctx, source.EnterpriseID, source.BaseID, source.Name)
	if err != nil {
		return nil, err
	}
	target := base.StructureTable{
		ID:           tableID,
		EnterpriseID: source.EnterpriseID,
		BaseID:       source.BaseID,
		Name:         name,
		Position:     count,
		CreatedAt:    service.now().UnixMilli(),
	}
	sourceFields, err := service.repository.FieldsByTable(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	sourceViews, err := service.repository.ViewsByTable(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	fieldIDs := make(map[string]string, len(sourceFields))
	fields := make([]base.StructureField, 0, len(sourceFields))
	optionsByField := make(map[string][]base.StructureOption, len(sourceFields))
	for _, sourceField := range sourceFields {
		fieldID, err := idgen.New()
		if err != nil {
			return nil, err
		}
		fieldIDs[sourceField.ID] = fieldID
		copy := sourceField
		copy.ID = fieldID
		copy.TableID = target.ID
		copy.CreatedAt = service.now().UnixMilli()
		config, err := base.DecodeStructureConfig(sourceField.Config)
		if err != nil {
			return nil, err
		}
		copy.Config, optionsByField[fieldID], err = service.remapCopiedFieldConfig(
			source,
			target.ID,
			fieldIDs,
			sourceField.Type,
			config,
		)
		if err != nil {
			return nil, err
		}
		fields = append(fields, copy)
	}
	views := make([]base.StructureView, 0, len(sourceViews))
	for _, sourceView := range sourceViews {
		config, err := decodeViewConfig(sourceView.Config)
		if err != nil {
			return nil, err
		}
		config = remapViewFieldIDs(config, fieldIDs)
		payload, err := encodeViewConfig(config)
		if err != nil {
			return nil, err
		}
		viewID, err := idgen.New()
		if err != nil {
			return nil, err
		}
		views = append(views, base.StructureView{
			ID:        viewID,
			TableID:   target.ID,
			Name:      sourceView.Name,
			Type:      sourceView.Type,
			Config:    payload,
			Position:  sourceView.Position,
			CreatedAt: service.now().UnixMilli(),
		})
	}
	if err := service.repository.DuplicateTableStructure(
		ctx,
		target,
		fields,
		optionsByField,
		views,
	); err != nil {
		return nil, err
	}
	if duplicator, ok := service.impact.(TableRecordsDuplicator); ok {
		if err := duplicator.DuplicateTableRecords(ctx, source.ID, target.ID); err != nil {
			return nil, err
		}
	}
	persisted, err := service.repository.TableByID(ctx, target.EnterpriseID, target.ID)
	if err != nil {
		return nil, err
	}
	return &structurev1.DuplicateTableResponse{Table: tableToProto(persisted)}, nil
}

func (service *Service) uniqueTableCopyName(
	ctx context.Context,
	enterpriseID, baseID, sourceName string,
) (string, error) {
	for copyIndex := 1; copyIndex <= 100; copyIndex++ {
		suffix := " 的副本"
		if copyIndex > 1 {
			suffix = fmt.Sprintf(" 的副本%d", copyIndex)
		}
		name := truncatedCopyName(sourceName, suffix)
		exists, err := service.repository.TableNameExists(ctx, enterpriseID, baseID, name, "")
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", rpcError(1001)
}

func (service *Service) remapCopiedFieldConfig(
	sourceTable base.StructureTable,
	targetID string,
	fieldIDs map[string]string,
	fieldType string,
	config map[string]any,
) ([]byte, []base.StructureOption, error) {
	if fieldType == "FIELD_TYPE_SELECT" || fieldType == "FIELD_TYPE_MULTISELECT" {
		rawOptions := []any{}
		if value, ok := config["option_list"].([]any); ok {
			rawOptions = value
		}
		options, normalized, err := normalizeOptions(rawOptions)
		if err != nil {
			return nil, nil, rpcError(1001)
		}
		config["option_list"] = normalized
		payload, err := base.EncodeStructureConfig(config)
		return payload, options, err
	}
	if fieldType == "FIELD_TYPE_RELATION" {
		if config["target_table_id"] == sourceTable.ID {
			config["target_table_id"] = targetID
		}
	}
	if fieldType == "FIELD_TYPE_LOOKUP" {
		if relationID := stringValue(config["relation_field_id"]); fieldIDs[relationID] != "" {
			config["relation_field_id"] = fieldIDs[relationID]
		}
		if targetFieldID := stringValue(config["target_field_id"]); fieldIDs[targetFieldID] != "" {
			config["target_field_id"] = fieldIDs[targetFieldID]
		}
	}
	payload, err := base.EncodeStructureConfig(config)
	return payload, nil, err
}

func remapViewFieldIDs(
	config *structurev1.ViewConfig,
	fieldIDs map[string]string,
) *structurev1.ViewConfig {
	if config == nil {
		return &structurev1.ViewConfig{}
	}
	config.HiddenFieldList = remapIDs(config.GetHiddenFieldList(), fieldIDs)
	config.FrozenFieldList = remapIDs(config.GetFrozenFieldList(), fieldIDs)
	for _, filter := range config.GetFilterList() {
		if mapped := fieldIDs[filter.GetFieldId()]; mapped != "" {
			filter.FieldId = mapped
		}
	}
	for _, sort := range config.GetSortList() {
		if mapped := fieldIDs[sort.GetFieldId()]; mapped != "" {
			sort.FieldId = mapped
		}
	}
	for _, group := range config.GetGroupByList() {
		if mapped := fieldIDs[group.GetFieldId()]; mapped != "" {
			group.FieldId = mapped
		}
	}
	return config
}

func remapIDs(ids []string, fieldIDs map[string]string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if mapped := fieldIDs[id]; mapped != "" {
			result = append(result, mapped)
		}
	}
	return result
}
