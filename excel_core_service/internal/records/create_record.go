// records/create_record.go：记录创建、字段校验、历史与变更事件。
package records

import (
	"context"
	"database/sql"
	"fmt"

	commonv1 "excel/api/common/v1"
	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// CreateRecord inserts one record with validation, history and record.changed
// in one transaction.
func (service *Service) CreateRecord(
	ctx context.Context,
	request *recordsv1.CreateRecordRequest,
) (*recordsv1.CreateRecordResponse, error) {
	if service == nil || service.repository == nil || request == nil ||
		request.GetTableId() == "" ||
		!validClientRequestID(request.GetClientRequestId()) {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := rowScopeFromContext(ctx); err != nil {
		return nil, err
	}
	if err := service.validateTableBaseMetadata(ctx, request.GetTableId()); err != nil {
		return nil, err
	}
	if payload, found, err := service.replayOplog(
		ctx,
		request.GetClientRequestId(),
		oplogKindCreateRecord,
	); err != nil {
		return nil, err
	} else if found {
		response := &recordsv1.CreateRecordResponse{}
		if err := unmarshalProtoResponse(payload, response); err != nil {
			return nil, err
		}
		return response, nil
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if request.GetClientRequestId() != "" {
		duplicate, reserveErr := service.reserveOplog(
			ctx,
			tx,
			request.GetClientRequestId(),
			oplogKindCreateRecord,
			request,
		)
		if reserveErr != nil {
			return nil, reserveErr
		}
		if duplicate {
			_ = tx.Rollback()
			payload, found, replayErr := service.replayOplog(
				ctx,
				request.GetClientRequestId(),
				oplogKindCreateRecord,
			)
			if replayErr != nil {
				return nil, replayErr
			}
			if !found {
				return nil, rpcError(deal.CodeServiceUnavailable)
			}
			response := &recordsv1.CreateRecordResponse{}
			if err := unmarshalProtoResponse(payload, response); err != nil {
				return nil, err
			}
			return response, nil
		}
	}
	record, err := service.createRecordInTx(
		ctx,
		tx,
		request.GetTableId(),
		request.GetCellsMap(),
		principal,
	)
	if err != nil {
		return nil, err
	}
	response := &recordsv1.CreateRecordResponse{Record: recordToProto(*record)}
	if request.GetClientRequestId() != "" {
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
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit CreateRecord: %w", err)
	}
	return response, nil
}

func (service *Service) createRecordInTx(
	ctx context.Context,
	tx *sql.Tx,
	tableID string,
	cells map[string]*commonv1.Value,
	principal *commonv1.Principal,
) (*base.Record, error) {
	if service.outbox == nil || service.bases == nil || service.structure == nil {
		return nil, rpcError(deal.CodeServiceUnavailable)
	}
	table, err := service.structure.Table(ctx, tableID)
	if err != nil {
		return nil, mapRecordError(err)
	}
	if table == nil || table.GetId() != tableID {
		return nil, rpcError(deal.CodeInvalidArgument)
	}
	fields, err := service.structure.Fields(ctx, tableID)
	if err != nil {
		return nil, mapRecordError(err)
	}
	byID := fieldByID(fields)
	validated := make(map[string]*commonv1.Value, len(cells))
	changedFields := make([]*structurev1.Field, 0, len(cells))
	for fieldID, value := range cells {
		field := byID[fieldID]
		if field == nil || !fieldIsWritable(field.GetType()) {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		if err := validateFieldValue(field, value); err != nil {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		validated[fieldID] = value
		changedFields = append(changedFields, field)
	}
	count, err := service.repository.CountActiveRecords(ctx, tx, tableID)
	if err != nil {
		return nil, err
	}
	if count >= maxRecordsPerTable {
		return nil, rpcError(deal.CodeCapacityLimit)
	}
	recordID, err := newRecordULID()
	if err != nil {
		return nil, err
	}
	record := &base.Record{
		ID:        recordID,
		TableID:   tableID,
		Cells:     cloneRecordCells(validated),
		CreatedBy: principal.GetUserId(),
		CreatedAt: service.now().UnixMilli(),
		Version:   1,
	}
	formulaChanges, err := service.evaluateFormulaClosure(
		ctx,
		tx,
		record,
		fields,
		changedFields,
	)
	if err != nil {
		return nil, err
	}
	if err := service.repository.CreateRecordTx(ctx, tx, *record); err != nil {
		return nil, err
	}
	for _, field := range changedFields {
		if err := service.appendHistory(
			ctx,
			tx,
			record.ID,
			field.GetId(),
			nil,
			record.Cells[field.GetId()],
			principal.GetUserId(),
		); err != nil {
			return nil, err
		}
	}
	for _, change := range formulaChanges {
		if err := service.appendHistory(
			ctx,
			tx,
			record.ID,
			change.FieldID,
			nil,
			change.After,
			principal.GetUserId(),
		); err != nil {
			return nil, err
		}
	}
	baseID, _, err := service.bases.ContextByTable(ctx, tableID)
	if err != nil {
		return nil, err
	}
	if err := validateBaseMetadata(ctx, baseID); err != nil {
		return nil, err
	}
	event, err := recordChangedEvent(
		baseID,
		tableID,
		record.ID,
		"",
		nil,
		record.Version,
		principal.GetUserId(),
		"create",
		service.now(),
		"records",
	)
	if err != nil {
		return nil, err
	}
	if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("enqueue record.changed: %w", err)
	}
	for _, field := range changedFields {
		if err := service.enqueueCalcJobIfNeeded(
			ctx,
			tx,
			baseID,
			tableID,
			record.ID,
			field.GetId(),
		); err != nil {
			return nil, err
		}
	}
	return record, nil
}
