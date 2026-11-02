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
	principal, _, err := service.memberPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetBaseId() == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	tables, err := service.repository.TablesByBase(
		ctx,
		principal.GetEnterpriseId(),
		request.GetBaseId(),
	)
	if err != nil {
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
