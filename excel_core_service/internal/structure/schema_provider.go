// structure/schema_provider.go：向 calc 提供完整字段依赖图。
package structure

import (
	"context"
	"errors"
	"fmt"

	"excel_core_service/base"
	"excel_core_service/internal/calc"
)

// SchemaProvider projects structure persistence into calc's public schema seam.
type SchemaProvider struct {
	repository *base.StructureRepository
}

// NewSchemaProvider creates the calc schema adapter.
func NewSchemaProvider(repository *base.StructureRepository) *SchemaProvider {
	return &SchemaProvider{repository: repository}
}

// Fields returns the complete cross-table structure graph.
func (provider *SchemaProvider) Fields(ctx context.Context) ([]calc.FieldDefinition, error) {
	if provider == nil || provider.repository == nil {
		return nil, errors.New("structure repository is required")
	}
	fields, err := provider.repository.AllFields(ctx)
	if err != nil {
		return nil, err
	}
	return definitionsFromStructureFields(fields)
}

func definitionsFromStructureFields(
	fields []base.StructureField,
) ([]calc.FieldDefinition, error) {
	primaryByTable := make(map[string]string)
	for _, field := range fields {
		if field.IsPrimary {
			primaryByTable[field.TableID] = field.ID
		}
	}
	definitions := make([]calc.FieldDefinition, 0, len(fields))
	for _, field := range fields {
		config, err := base.DecodeStructureConfig(field.Config)
		if err != nil {
			return nil, err
		}
		definition := calc.FieldDefinition{
			ID:      field.ID,
			Name:    field.Name,
			TableID: field.TableID,
			Type:    field.Type,
		}
		switch field.Type {
		case "FIELD_TYPE_FORMULA":
			definition.Formula = stringValue(config["expression"])
		case "FIELD_TYPE_RELATION":
			targetTableID := stringValue(config["target_table_id"])
			targetFieldID := primaryByTable[targetTableID]
			if targetTableID == "" || targetFieldID == "" {
				return nil, fmt.Errorf("relation field %s has no target primary field", field.ID)
			}
			definition.Relation = &calc.RelationTarget{
				TableID: targetTableID,
				FieldID: targetFieldID,
			}
		case "FIELD_TYPE_LOOKUP":
			definition.Lookup = &calc.LookupSource{
				RelationFieldID: stringValue(config["relation_field_id"]),
				TargetFieldID:   stringValue(config["target_field_id"]),
			}
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}
