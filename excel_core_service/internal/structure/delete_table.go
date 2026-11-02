package structure

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	structurev1 "excel/api/structure/v1"
)

// DeleteTable removes a table and all structure-owned children, keeping at
// least one table in the Base.
func (service *Service) DeleteTable(
	ctx context.Context,
	request *structurev1.DeleteTableRequest,
) (*emptypb.Empty, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	table, err := service.repository.TableByID(
		ctx,
		principal.GetEnterpriseId(),
		request.GetTableId(),
	)
	if err != nil {
		return nil, mapStructureError(err)
	}
	count, err := service.repository.CountTables(ctx, table.EnterpriseID, table.BaseID)
	if err != nil {
		return nil, err
	}
	if count <= 1 {
		return nil, rpcError(1001)
	}
	if !request.GetConfirm() {
		return &emptypb.Empty{}, nil
	}
	if deleter, ok := service.impact.(TableRecordsDeleter); ok {
		if err := deleter.DeleteTableRecords(ctx, table.ID); err != nil {
			return nil, err
		}
	}
	if err := service.repository.DeleteTable(ctx, table.ID); err != nil {
		return nil, mapStructureError(err)
	}
	return &emptypb.Empty{}, nil
}
