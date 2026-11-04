package structure

import (
	"context"
	"excel_core_service/deal"
	"fmt"
	"strings"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

// DuplicateField copies field configuration without copying record values.
func (service *Service) DuplicateField(
	ctx context.Context,
	request *structurev1.DuplicateFieldRequest,
) (*structurev1.DuplicateFieldResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	source, err := service.repository.FieldByID(ctx, request.GetFieldId())
	if err != nil {
		return nil, mapStructureError(err)
	}
	table, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		source.TableID,
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	count, err := service.repository.CountFields(ctx, table.ID)
	if err != nil {
		return nil, err
	}
	if count >= maxFieldsPerTable {
		return nil, deal.RPCError(5001)
	}
	name, err := service.uniqueFieldCopyName(ctx, table.ID, source.Name)
	if err != nil {
		return nil, err
	}
	config, err := base.DecodeStructureConfig(source.Config)
	if err != nil {
		return nil, err
	}
	options := []base.StructureOption{}
	if source.Type == "FIELD_TYPE_SELECT" || source.Type == "FIELD_TYPE_MULTISELECT" {
		rawOptions := []any{}
		if value, ok := config["option_list"].([]any); ok {
			rawOptions = value
		}
		generatedOptions, normalizedOptions, err := normalizeOptions(rawOptions)
		if err != nil {
			return nil, deal.RPCError(1001)
		}
		options = generatedOptions
		config["option_list"] = normalizedOptions
	} else {
		config, options, err = service.normalizeFieldConfig(
			ctx,
			table,
			structurev1.FieldType(structurev1.FieldType_value[source.Type]),
			config,
		)
		if err != nil {
			return nil, deal.RPCError(1001)
		}
	}
	payload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	fieldID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	copy := base.StructureField{
		ID:        fieldID,
		TableID:   source.TableID,
		Name:      name,
		Type:      source.Type,
		Config:    payload,
		IsPrimary: false,
		Position:  count,
		CreatedAt: service.now().UnixMilli(),
	}
	if copy.Type == "FIELD_TYPE_FORMULA" ||
		copy.Type == "FIELD_TYPE_RELATION" ||
		copy.Type == "FIELD_TYPE_LOOKUP" {
		fields, err := service.repository.AllFields(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateProposedGraph(ctx, service.calcValidator, append(fields, copy)); err != nil {
			return nil, err
		}
	}
	if err := service.repository.CreateField(ctx, copy, options); err != nil {
		return nil, err
	}
	projected, err := fieldToProto(copy)
	if err != nil {
		return nil, err
	}
	return &structurev1.DuplicateFieldResponse{Field: projected}, nil
}

func (service *Service) uniqueFieldCopyName(
	ctx context.Context,
	tableID, sourceName string,
) (string, error) {
	for copyIndex := 1; copyIndex <= 100; copyIndex++ {
		suffix := " 的副本"
		if copyIndex > 1 {
			suffix = fmt.Sprintf(" 的副本%d", copyIndex)
		}
		name := truncatedCopyName(sourceName, suffix)
		exists, err := service.repository.FieldNameExists(ctx, tableID, name, "")
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", deal.RPCError(1001)
}

func truncatedCopyName(sourceName, suffix string) string {
	maxBaseRunes := maxStructureNameRunes - len([]rune(suffix))
	runes := []rune(strings.TrimSpace(sourceName))
	if len(runes) > maxBaseRunes {
		runes = runes[:maxBaseRunes]
	}
	return string(runes) + suffix
}
