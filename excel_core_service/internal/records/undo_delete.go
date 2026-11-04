// records/undo_delete.go：七秒删除撤销。
package records

import (
	"context"
	"fmt"

	recordsv1 "excel/api/records/v1"
	"excel_core_service/deal"
)

// UndoDelete restores one soft-deleted record inside the seven-second window.
func (service *Service) UndoDelete(
	ctx context.Context,
	request *recordsv1.UndoDeleteRequest,
) (*recordsv1.UndoDeleteResponse, error) {
	if service == nil || service.repository == nil || request == nil ||
		request.GetRecordId() == "" {
		return nil, deal.RPCError(deal.CodeInvalidArgument)
	}
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	scope, err := rowScopeFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if service.outbox == nil || service.bases == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := service.repository.RecordByIDForUpdate(ctx, tx, request.GetRecordId())
	if err != nil || state.SoftDeletedAt == nil {
		return nil, deal.RPCError(deal.CodeDeletedResource)
	}
	if scope == rowScopeOwn && state.CreatedBy != principal.GetUserId() {
		return nil, deal.RPCError(deal.CodeDeletedResource)
	}
	if err := service.validateTableBaseMetadata(ctx, state.TableID); err != nil {
		return nil, err
	}
	if service.now().UnixMilli()-*state.SoftDeletedAt > 7000 {
		return nil, deal.RPCError(deal.CodeDeletedResource)
	}
	nextVersion := state.Version + 1
	if err := service.repository.RestoreRecordTx(ctx, tx, state.ID, nextVersion); err != nil {
		return nil, err
	}
	if err := service.appendHistory(
		ctx,
		tx,
		state.ID,
		recordDeletedFieldID,
		stringCellValue("deleted"),
		nil,
		principal.GetUserId(),
	); err != nil {
		return nil, err
	}
	baseID, _, err := service.bases.ContextByTable(ctx, state.TableID)
	if err != nil {
		return nil, err
	}
	if err := validateBaseMetadata(ctx, baseID); err != nil {
		return nil, err
	}
	event, err := recordChangedEvent(
		baseID,
		state.TableID,
		state.ID,
		"",
		nil,
		nextVersion,
		principal.GetUserId(),
		"undo_delete",
		service.now(),
		"records",
	)
	if err != nil {
		return nil, err
	}
	if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("enqueue record.changed: %w", err)
	}
	state.Version = nextVersion
	state.SoftDeletedAt = nil
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit UndoDelete: %w", err)
	}
	return &recordsv1.UndoDeleteResponse{
		Record: recordToProto(state.Record),
	}, nil
}
