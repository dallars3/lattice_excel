// records/get_record.go：单记录读取与派生单元格可见性过滤。
package records

import (
	"context"
	"errors"
	"strings"

	recordsv1 "excel/api/records/v1"
	"excel_core_service/deal"
)

// GetRecord returns one active record visible in the caller's row scope.
func (service *Service) GetRecord(
	ctx context.Context,
	request *recordsv1.GetRecordRequest,
) (*recordsv1.GetRecordResponse, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("records repository is not configured")
	}
	if request == nil || strings.TrimSpace(request.GetRecordId()) == "" {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	scope, err := rowScopeFromContext(ctx)
	if err != nil {
		return nil, err
	}
	record, err := service.repository.RecordByID(ctx, request.GetRecordId())
	if err != nil {
		return nil, mapRecordError(err)
	}
	if scope == rowScopeOwn &&
		record.CreatedBy != principal.GetUserId() {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if service.structure == nil {
		return nil, errors.New("records structure accessor is not configured")
	}
	table, err := service.structure.Table(ctx, record.TableID)
	if err != nil {
		return nil, mapRecordError(err)
	}
	if table == nil || table.GetId() != record.TableID {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	fields, err := service.structure.Fields(ctx, table.GetId())
	if err != nil {
		return nil, mapRecordError(err)
	}
	if err := service.decorateRecord(
		ctx,
		principal,
		table.GetBaseId(),
		&record,
		fieldByID(fields),
	); err != nil {
		return nil, err
	}
	return &recordsv1.GetRecordResponse{Record: recordToProto(record)}, nil
}
