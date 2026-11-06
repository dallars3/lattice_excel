package structure

import (
	"context"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/deal"
)

// ListFields lists fields in table display order.
func (service *Service) ListFields(
	ctx context.Context,
	request *structurev1.ListFieldsRequest,
) (*structurev1.ListFieldsResponse, error) {
	principal, err := service.readPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetTableId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	if _, err := service.tableForReadTenancy(ctx, principal, request.GetTableId()); err != nil {
		return nil, mapStructureError(err)
	}
	fields, err := service.repository.FieldsByTable(ctx, request.GetTableId())
	if err != nil {
		return nil, err
	}
	response := &structurev1.ListFieldsResponse{
		FieldList: make([]*structurev1.Field, 0, len(fields)),
	}
	for _, field := range fields {
		projected, err := fieldToProto(field)
		if err != nil {
			return nil, err
		}
		response.FieldList = append(response.FieldList, projected)
	}
	return response, nil
}
