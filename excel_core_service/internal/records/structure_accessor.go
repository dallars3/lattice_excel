// records/structure_accessor.go：把 structure 持久化投影适配为读链所需的
// structurev1 原型视图，避免 records 直接解释 excel_structure_* 的存储格式。
package records

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
)

type structureViewAccessor struct {
	repository *base.StructureRepository
}

// NewStructureViewAccessor adapts the structure repository to SchemaOrViewAccessor.
func NewStructureViewAccessor(repository *base.StructureRepository) SchemaOrViewAccessor {
	return structureViewAccessor{repository: repository}
}

func (accessor structureViewAccessor) Table(
	ctx context.Context,
	tableID string,
) (*structurev1.Table, error) {
	table, err := accessor.repository.TableByID(ctx, tableID)
	if err != nil {
		return nil, fmt.Errorf("load table %s: %w", tableID, err)
	}
	return &structurev1.Table{
		Id:         table.ID,
		BaseId:     table.BaseID,
		Name:       table.Name,
		FieldCount: int32(table.FieldCount),
		ViewCount:  int32(table.ViewCount),
		CreatedAt:  table.CreatedAt,
	}, nil
}

func (accessor structureViewAccessor) Fields(
	ctx context.Context,
	tableID string,
) ([]*structurev1.Field, error) {
	fields, err := accessor.repository.FieldsByTable(ctx, tableID)
	if err != nil {
		return nil, fmt.Errorf("load fields for table %s: %w", tableID, err)
	}
	protoFields := make([]*structurev1.Field, 0, len(fields))
	for _, field := range fields {
		protoField, fieldErr := accessor.fieldToProto(field)
		if fieldErr != nil {
			return nil, fieldErr
		}
		protoFields = append(protoFields, protoField)
	}
	return protoFields, nil
}

func (accessor structureViewAccessor) View(
	ctx context.Context,
	viewID string,
) (*structurev1.View, *structurev1.ViewConfig, error) {
	view, err := accessor.repository.ViewByID(ctx, viewID)
	if err != nil {
		return nil, nil, fmt.Errorf("load view %s: %w", viewID, err)
	}
	config := &structurev1.ViewConfig{}
	if len(view.Config) > 0 {
		if err := protojson.Unmarshal(view.Config, config); err != nil {
			return nil, nil, fmt.Errorf("decode view %s config: %w", viewID, err)
		}
	}
	return &structurev1.View{
		Id:        view.ID,
		TableId:   view.TableID,
		Name:      view.Name,
		Type:      view.Type,
		CreatedAt: view.CreatedAt,
	}, config, nil
}

func (accessor structureViewAccessor) fieldToProto(
	field base.StructureField,
) (*structurev1.Field, error) {
	config, err := base.DecodeStructureConfig(field.Config)
	if err != nil {
		return nil, fmt.Errorf("decode field %s config: %w", field.ID, err)
	}
	configMap, err := structpb.NewStruct(config)
	if err != nil {
		return nil, fmt.Errorf("project field %s config: %w", field.ID, err)
	}
	fieldType, ok := structurev1.FieldType_value[field.Type]
	if !ok {
		return nil, fmt.Errorf("field %s has unknown type %q", field.ID, field.Type)
	}
	return &structurev1.Field{
		Id:        field.ID,
		TableId:   field.TableID,
		Name:      field.Name,
		Type:      structurev1.FieldType(fieldType),
		ConfigMap: configMap,
		IsPrimary: field.IsPrimary,
		Position:  int32(field.Position),
	}, nil
}
