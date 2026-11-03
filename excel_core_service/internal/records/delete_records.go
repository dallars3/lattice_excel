// records/delete_records.go：软删除、删除历史与 record.deleted 事件。
package records

import (
	"context"
	"fmt"

	recordsv1 "excel/api/records/v1"
	"excel_core_service/deal"
)

const recordDeletedFieldID = "00000000000000000000000DEL"

type deletedEventGroup struct {
	baseID       string
	enterpriseID string
	tableID      string
	recordIDs    []string
}

// DeleteRecords soft-deletes visible records and publishes record.deleted in
// the same transaction.
func (service *Service) DeleteRecords(
	ctx context.Context,
	request *recordsv1.DeleteRecordsRequest,
) (*recordsv1.DeleteRecordsResponse, error) {
	if service == nil || service.repository == nil || request == nil ||
		len(request.GetRecordIdList()) == 0 ||
		!validClientRequestID(request.GetClientRequestId()) {
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
		oplogKindDeleteRecords,
	); err != nil {
		return nil, err
	} else if found {
		response := &recordsv1.DeleteRecordsResponse{}
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
	if request.GetClientRequestId() != "" {
		duplicate, reserveErr := service.reserveOplog(
			ctx,
			tx,
			request.GetClientRequestId(),
			oplogKindDeleteRecords,
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
				oplogKindDeleteRecords,
			)
			if replayErr != nil {
				return nil, replayErr
			}
			if !found {
				return nil, rpcError(deal.CodeServiceUnavailable)
			}
			response := &recordsv1.DeleteRecordsResponse{}
			if err := unmarshalProtoResponse(payload, response); err != nil {
				return nil, err
			}
			return response, nil
		}
	}

	seen := make(map[string]struct{}, len(request.GetRecordIdList()))
	groups := make(map[string]*deletedEventGroup)
	groupOrder := make([]string, 0)
	deletedAt := service.now().UnixMilli()
	deletedCount := int32(0)
	for _, recordID := range request.GetRecordIdList() {
		if recordID == "" {
			return nil, rpcError(deal.CodeInvalidArgument)
		}
		if _, duplicate := seen[recordID]; duplicate {
			continue
		}
		seen[recordID] = struct{}{}
		state, err := service.repository.RecordByIDForUpdate(ctx, tx, recordID)
		if err != nil || state.SoftDeletedAt != nil {
			return nil, rpcError(deal.CodeDeletedResource)
		}
		if scope == rowScopeOwn && state.CreatedBy != principal.GetUserId() {
			return nil, rpcError(deal.CodeDeletedResource)
		}
		baseID, enterpriseID, err := service.bases.ContextByTable(ctx, state.TableID)
		if err != nil {
			return nil, err
		}
		if err := validateBaseMetadata(ctx, baseID); err != nil {
			return nil, err
		}
		nextVersion := state.Version + 1
		if err := service.repository.SoftDeleteRecordTx(
			ctx,
			tx,
			state.ID,
			nextVersion,
			deletedAt,
		); err != nil {
			return nil, err
		}
		if err := service.appendHistory(
			ctx,
			tx,
			state.ID,
			recordDeletedFieldID,
			nil,
			stringCellValue("deleted"),
			principal.GetUserId(),
		); err != nil {
			return nil, err
		}
		key := baseID + "\x00" + enterpriseID + "\x00" + state.TableID
		group := groups[key]
		if group == nil {
			group = &deletedEventGroup{
				baseID:       baseID,
				enterpriseID: enterpriseID,
				tableID:      state.TableID,
			}
			groups[key] = group
			groupOrder = append(groupOrder, key)
		}
		group.recordIDs = append(group.recordIDs, state.ID)
		deletedCount++
	}
	for _, key := range groupOrder {
		group := groups[key]
		event, err := recordDeletedEvent(
			group.baseID,
			group.tableID,
			group.enterpriseID,
			group.recordIDs,
			service.now(),
		)
		if err != nil {
			return nil, err
		}
		if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("enqueue record.deleted: %w", err)
		}
	}
	response := &recordsv1.DeleteRecordsResponse{DeletedCount: deletedCount}
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
		return nil, fmt.Errorf("commit DeleteRecords: %w", err)
	}
	return response, nil
}
