package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// ListTables lists all data tables in a Base.
func (service *Service) ListTables(
	ctx context.Context,
	request *structurev1.ListTablesRequest,
) (*structurev1.ListTablesResponse, error) {
	principal, err := service.readPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	// 先解析再裁决：协作者回退路径（外企业 Base 协作者）必须把 collaboratorPath
	// 交给 authorizeRead 做 Base 级裁决（fail-closed），数据未过裁决不返回。
	tables, collaboratorPath, err := service.tablesInBaseForRead(ctx, principal, request.GetBaseId())
	if err != nil {
		return nil, err
	}
	if err := service.authorizeRead(ctx, principal, request.GetBaseId(), collaboratorPath); err != nil {
		return nil, err
	}
	response := &structurev1.ListTablesResponse{
		TableList: make([]*structurev1.Table, 0, len(tables)),
	}
	for _, table := range tables {
		response.TableList = append(response.TableList, tableToProto(table))
	}
	return response, nil
}
