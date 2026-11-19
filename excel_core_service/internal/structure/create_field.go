package structure

import (
	"context"
	"fmt"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// CreateField creates one typed field after strict config and graph validation.
func (service *Service) CreateField(
	ctx context.Context,
	request *structurev1.CreateFieldRequest,
) (*structurev1.CreateFieldResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetTableId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	table, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		request.GetTableId(),
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	name, err := normalizeStructureName(request.GetName())
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	description, err := normalizeFieldDescription(request.GetDescription())
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	fieldType, ok := fieldTypeString(request.GetType())
	if !ok {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	count, err := service.repository.CountFields(ctx, table.ID)
	if err != nil {
		return nil, err
	}
	if count >= maxFieldsPerTable {
		return nil, deal.RPCError(deal.CodeCapacityLimit)
	}
	exists, err := service.repository.FieldNameExists(ctx, table.ID, name, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	input := map[string]any{}
	if request.GetConfigMap() != nil {
		input = request.GetConfigMap().AsMap()
	}
	config, options, err := service.normalizeFieldConfig(ctx, table, request.GetType(), input)
	if err != nil {
		return nil, mapFieldConfigError(err)
	}
	configPayload, err := base.EncodeStructureConfig(config)
	if err != nil {
		return nil, err
	}
	fieldID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	field := base.StructureField{
		ID:          fieldID,
		TableID:     table.ID,
		Name:        name,
		Type:        fieldType,
		Config:      configPayload,
		Description: description,
		IsPrimary:   count == 0,
		Position:    count,
		CreatedAt:   service.now().UnixMilli(),
	}
	if isComputedField(field.Type) {
		allFields, err := service.repository.AllFields(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateProposedGraph(
			ctx,
			service.calcValidator,
			append(allFields, field),
		); err != nil {
			return nil, err
		}
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.CreateFieldTx(ctx, tx, field, options); err != nil {
		return nil, mapStructureError(err)
	}
	// 缺陷 #18（W-11）：字段写成功即与写同事务发布结构事件，房间内其他连接
	// 收到 structure_changed 且断线重放可补齐。
	if err := service.publishStructureChanged(
		ctx,
		tx,
		table,
		field.ID,
		structureChangeFieldCreated,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	// N5 缺陷：存量记录不会因编辑再触发新公式/引用列的物化——创建成功即与
	// 写同事务入队 calc 回填作业，存量行的列值由 calc 消费者分批幂等补齐。
	if needsCalcBackfill(field.Type) {
		if err := service.enqueueCalcBackfill(ctx, tx, table, field.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit CreateField: %w", err)
	}
	projected, err := fieldToProto(field)
	if err != nil {
		return nil, err
	}
	return &structurev1.CreateFieldResponse{Field: projected}, nil
}
