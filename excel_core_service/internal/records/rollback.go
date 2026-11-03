// records/rollback.go：历史字段回滚与新节点追加。
package records

import (
	"context"
	"errors"
	"fmt"

	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// Rollback restores one visible field's historical before value by appending a
// new history node rather than mutating the old chain.
func (service *Service) Rollback(
	ctx context.Context,
	request *recordsv1.RollbackRequest,
) (*recordsv1.RollbackResponse, error) {
	if service == nil || service.repository == nil || request == nil ||
		request.GetNodeId() == "" {
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
	history, err := service.repository.HistoryByID(ctx, request.GetNodeId())
	if errors.Is(err, base.ErrRecordNotFound) {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if err != nil {
		return nil, err
	}
	if history.CreatedAt < service.now().Add(-historyRetention).UnixMilli() {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if service.outbox == nil || service.bases == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := service.repository.RecordByIDForUpdate(
		ctx,
		tx,
		history.RecordID,
	)
	if err != nil || state.SoftDeletedAt != nil {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if scope == rowScopeOwn && state.CreatedBy != principal.GetUserId() {
		return nil, rpcError(deal.CodeDeletedResource)
	}
	if err := service.validateTableBaseMetadata(ctx, state.TableID); err != nil {
		return nil, err
	}
	fields, err := service.structure.Fields(ctx, state.TableID)
	if err != nil {
		return nil, mapRecordError(err)
	}
	field := fieldByID(fields)[history.FieldID]
	if field == nil ||
		field.GetType() == structurev1.FieldType_FIELD_TYPE_FORMULA ||
		field.GetType() == structurev1.FieldType_FIELD_TYPE_LOOKUP ||
		field.GetType() == structurev1.FieldType_FIELD_TYPE_RELATION {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	if history.Before != nil {
		if err := validateFieldValue(field, history.Before); err != nil {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
	}
	updated, err := service.updateLockedCell(
		ctx,
		tx,
		state,
		field,
		history.Before,
		principal.GetUserId(),
		"rollback",
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Rollback: %w", err)
	}
	return &recordsv1.RollbackResponse{Record: recordToProto(updated)}, nil
}
