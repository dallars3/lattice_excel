package structure

import (
	"context"

	"excel-libs/idgen"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// 默认结构口径与前端 bootstrapTableDefaults 兜底一致（AC-TBL-01）。
const (
	defaultPrimaryFieldName = "标题"
	defaultTableViewName    = "表格"
	defaultTableViewType    = "table"
)

// CreateTable creates one data table within a Base, together with the default
// primary field and default view, in one repository transaction.
func (service *Service) CreateTable(
	ctx context.Context,
	request *structurev1.CreateTableRequest,
) (*structurev1.CreateTableResponse, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	name, err := normalizeStructureName(request.GetName())
	if err != nil {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	count, err := service.repository.CountTables(ctx, principal.GetEnterpriseId(), request.GetBaseId())
	if err != nil {
		return nil, err
	}
	if count >= maxTablesPerBase {
		return nil, deal.RPCError(deal.CodeCapacityLimit)
	}
	exists, err := service.repository.TableNameExists(
		ctx,
		principal.GetEnterpriseId(),
		request.GetBaseId(),
		name,
		"",
	)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, deal.RPCError(deal.CodeConflict)
	}
	id, err := idgen.New()
	if err != nil {
		return nil, err
	}
	fieldID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	viewID, err := idgen.New()
	if err != nil {
		return nil, err
	}
	now := service.now().UnixMilli()
	fieldConfig, err := base.EncodeStructureConfig(map[string]any{})
	if err != nil {
		return nil, err
	}
	viewConfig, err := encodeViewConfig(&structurev1.ViewConfig{})
	if err != nil {
		return nil, err
	}
	table := base.StructureTable{
		ID:           id,
		EnterpriseID: principal.GetEnterpriseId(),
		BaseID:       request.GetBaseId(),
		Name:         name,
		Position:     count,
		FieldCount:   1,
		ViewCount:    1,
		CreatedAt:    now,
	}
	primaryField := base.StructureField{
		ID:        fieldID,
		Name:      defaultPrimaryFieldName,
		Type:      structurev1.FieldType_FIELD_TYPE_TEXT.String(),
		Config:    fieldConfig,
		IsPrimary: true,
		Position:  0,
		CreatedAt: now,
	}
	defaultView := base.StructureView{
		ID:        viewID,
		Name:      defaultTableViewName,
		Type:      defaultTableViewType,
		Config:    viewConfig,
		Position:  0,
		CreatedAt: now,
	}
	if err := service.repository.CreateTableWithDefaults(ctx, table, primaryField, defaultView); err != nil {
		return nil, err
	}
	return &structurev1.CreateTableResponse{Table: tableToProto(table)}, nil
}
