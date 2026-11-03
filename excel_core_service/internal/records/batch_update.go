// records/batch_update.go：批量写入、逐 op 结果与幂等重放。
package records

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

const (
	oplogKindCreateRecord  = "CreateRecord"
	oplogKindBatchUpdate   = "BatchUpdate"
	oplogKindDeleteRecords = "DeleteRecords"
	maxRecordsPerTable     = 50000
)

// BatchUpdate applies operations serially while preserving each operation's
// independent result. Skipped operations are read-only cells or option
// mismatches; failed rows carry a business error code.
func (service *Service) BatchUpdate(
	ctx context.Context,
	request *recordsv1.BatchUpdateRequest,
) (*recordsv1.BatchUpdateResponse, error) {
	if service == nil || service.repository == nil || request == nil ||
		request.GetClientRequestId() == "" ||
		!validClientRequestID(request.GetClientRequestId()) ||
		len(request.GetOpList()) == 0 {
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
	if payload, found, err := service.replayOplog(
		ctx,
		request.GetClientRequestId(),
		oplogKindBatchUpdate,
	); err != nil {
		return nil, err
	} else if found {
		response := &recordsv1.BatchUpdateResponse{}
		if err := unmarshalProtoResponse(payload, response); err != nil {
			return nil, err
		}
		return response, nil
	}
	if service.outbox == nil || service.bases == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}

	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	duplicate, err := service.reserveOplog(
		ctx,
		tx,
		request.GetClientRequestId(),
		oplogKindBatchUpdate,
		request,
	)
	if err != nil {
		return nil, err
	}
	if duplicate {
		_ = tx.Rollback()
		payload, found, replayErr := service.replayOplog(
			ctx,
			request.GetClientRequestId(),
			oplogKindBatchUpdate,
		)
		if replayErr != nil {
			return nil, replayErr
		}
		if !found {
			return nil, rpcError(deal.CodeServiceUnavailable)
		}
		response := &recordsv1.BatchUpdateResponse{}
		if err := unmarshalProtoResponse(payload, response); err != nil {
			return nil, err
		}
		return response, nil
	}

	response := &recordsv1.BatchUpdateResponse{}
	for index, operation := range request.GetOpList() {
		savepoint := fmt.Sprintf("records_batch_%d", index)
		if err := service.repository.Savepoint(ctx, tx, savepoint); err != nil {
			return nil, err
		}
		result, failed, skipped, fatalErr := service.applyBatchOperation(
			ctx,
			tx,
			int32(index),
			operation,
			principal,
			scope,
		)
		if fatalErr != nil {
			return nil, fatalErr
		}
		switch {
		case skipped:
			if err := service.repository.RollbackToSavepoint(ctx, tx, savepoint); err != nil {
				return nil, err
			}
			response.SkippedCount++
			response.ResultList = append(response.ResultList, result)
		case failed != nil:
			if err := service.repository.RollbackToSavepoint(ctx, tx, savepoint); err != nil {
				return nil, err
			}
			response.ResultList = append(response.ResultList, result)
			response.FailRowList = append(response.FailRowList, failed)
		default:
			response.ResultList = append(response.ResultList, result)
		}
		if err := service.repository.ReleaseSavepoint(ctx, tx, savepoint); err != nil {
			return nil, err
		}
	}
	responseJSON, err := marshalProtoResponse(response)
	if err != nil {
		return nil, err
	}
	if err := service.repository.UpdateOplogResponseTx(
		ctx,
		tx,
		request.GetClientRequestId(),
		responseJSON,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit BatchUpdate: %w", err)
	}
	return response, nil
}

func (service *Service) applyBatchOperation(
	ctx context.Context,
	tx *sql.Tx,
	index int32,
	operation *recordsv1.BatchOperation,
	principal *commonv1.Principal,
	scope string,
) (*recordsv1.OpResult, *recordsv1.OpResult, bool, error) {
	result := &recordsv1.OpResult{
		Index:    index,
		RecordId: operation.GetRecordId(),
	}
	if operation == nil || operation.GetFieldId() == "" {
		failed := failedResult(result, deal.CodeInvalidArgument)
		return failed, failedResult(result, deal.CodeInvalidArgument), false, nil
	}
	field, err := service.fieldByID(ctx, operation.GetFieldId())
	if err != nil {
		code := recordBusinessCode(err)
		return failedResult(result, code), failedResult(result, code), false, nil
	}
	if !fieldIsWritable(field.GetType()) {
		return failedResult(result, 0), nil, true, nil
	}
	if err := validateFieldValue(field, operation.GetValue()); err != nil {
		if field.GetType() == structurev1.FieldType_FIELD_TYPE_SELECT ||
			field.GetType() == structurev1.FieldType_FIELD_TYPE_MULTISELECT {
			return failedResult(result, 0), nil, true, nil
		}
		failed := failedResult(result, deal.CodeInvalidArgument)
		return failed, failedResult(result, deal.CodeInvalidArgument), false, nil
	}
	if operation.GetRecordId() == "" {
		record, err := service.createRecordInTx(
			ctx,
			tx,
			field.GetTableId(),
			map[string]*commonv1.Value{field.GetId(): operation.GetValue()},
			principal,
		)
		if err != nil {
			code := recordBusinessCode(err)
			failed := failedResult(result, code)
			return failed, failedResult(result, code), false, nil
		}
		result.RecordId = record.ID
		result.Ok = true
		result.Version = record.Version
		return result, nil, false, nil
	}

	state, err := service.repository.RecordByIDForUpdate(
		ctx,
		tx,
		operation.GetRecordId(),
	)
	if errors.Is(err, base.ErrRecordNotFound) || state.SoftDeletedAt != nil {
		failed := failedResult(result, deal.CodeDeletedResource)
		return failed, failedResult(result, deal.CodeDeletedResource), false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if state.TableID != field.GetTableId() {
		failed := failedResult(result, deal.CodeInvalidArgument)
		return failed, failedResult(result, deal.CodeInvalidArgument), false, nil
	}
	if scope == rowScopeOwn && state.CreatedBy != principal.GetUserId() {
		failed := failedResult(result, deal.CodeDeletedResource)
		return failed, failedResult(result, deal.CodeDeletedResource), false, nil
	}
	if err := service.validateTableBaseMetadata(ctx, state.TableID); err != nil {
		code := recordBusinessCode(err)
		failed := failedResult(result, code)
		return failed, failedResult(result, code), false, nil
	}
	if operation.GetExpectVersion() != 0 &&
		operation.GetExpectVersion() != state.Version {
		failed := failedResult(result, deal.CodeConcurrentConflict)
		return failed, failedResult(result, deal.CodeConcurrentConflict), false, nil
	}
	updated, err := service.updateLockedCell(
		ctx,
		tx,
		state,
		field,
		operation.GetValue(),
		principal.GetUserId(),
		"update",
	)
	if err != nil {
		code := recordBusinessCode(err)
		failed := failedResult(result, code)
		return failed, failedResult(result, code), false, nil
	}
	result.Ok = true
	result.Version = updated.Version
	return result, nil, false, nil
}

func failedResult(
	result *recordsv1.OpResult,
	code int,
) *recordsv1.OpResult {
	return &recordsv1.OpResult{
		Index:     result.GetIndex(),
		RecordId:  result.GetRecordId(),
		Version:   result.GetVersion(),
		ErrorCode: int32(code),
	}
}

func (service *Service) updateLockedCell(
	ctx context.Context,
	tx *sql.Tx,
	state base.RecordState,
	field *structurev1.Field,
	value *commonv1.Value,
	operator string,
	changeType string,
) (base.Record, error) {
	fields, err := service.structure.Fields(ctx, state.TableID)
	if err != nil {
		return base.Record{}, mapRecordError(err)
	}
	baseID, _, err := service.bases.ContextByTable(ctx, state.TableID)
	if err != nil {
		return base.Record{}, err
	}
	if err := validateBaseMetadata(ctx, baseID); err != nil {
		return base.Record{}, err
	}
	before := state.Cells[field.GetId()]
	next := state.Record
	next.Cells = cloneRecordCells(state.Cells)
	next.Version = state.Version + 1
	setRecordCell(next.Cells, field.GetId(), value)
	formulaChanges, err := service.evaluateFormulaClosure(
		ctx,
		tx,
		&next,
		fields,
		[]*structurev1.Field{field},
	)
	if err != nil {
		return base.Record{}, err
	}
	if err := service.repository.UpdateRecordTx(ctx, tx, next); err != nil {
		return base.Record{}, err
	}
	if err := service.appendHistory(
		ctx,
		tx,
		next.ID,
		field.GetId(),
		before,
		value,
		operator,
	); err != nil {
		return base.Record{}, err
	}
	for _, change := range formulaChanges {
		if err := service.appendHistory(
			ctx,
			tx,
			next.ID,
			change.FieldID,
			change.Before,
			change.After,
			operator,
		); err != nil {
			return base.Record{}, err
		}
	}
	event, err := recordChangedEvent(
		baseID,
		next.TableID,
		next.ID,
		field.GetId(),
		value,
		next.Version,
		operator,
		changeType,
		service.now(),
		"records",
	)
	if err != nil {
		return base.Record{}, err
	}
	if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
		return base.Record{}, fmt.Errorf("enqueue record.changed: %w", err)
	}
	if err := service.enqueueCalcJobIfNeeded(
		ctx,
		tx,
		baseID,
		next.TableID,
		next.ID,
		field.GetId(),
	); err != nil {
		return base.Record{}, err
	}
	return next, nil
}

func (service *Service) appendHistory(
	ctx context.Context,
	tx *sql.Tx,
	recordID, fieldID string,
	before, after *commonv1.Value,
	operator string,
) error {
	historyID, err := newRecordULID()
	if err != nil {
		return err
	}
	return service.repository.AppendHistoryTx(ctx, tx, base.RecordHistory{
		ID:        historyID,
		RecordID:  recordID,
		FieldID:   fieldID,
		Before:    before,
		After:     after,
		Operator:  operator,
		CreatedAt: service.now().UnixMilli(),
	})
}
