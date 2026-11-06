package structure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"excel-libs/eventbus"
	"excel-libs/idgen"

	"google.golang.org/protobuf/types/known/emptypb"

	structurev1 "excel/api/structure/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
)

// DeleteTable removes a table and all structure-owned children, keeping at
// least one table in the Base. 删除与 table.deleted 事件在同一事务内提交
// （缺陷 #23：表删除必须进入企业审计，事件丢失即审计缺失）。
func (service *Service) DeleteTable(
	ctx context.Context,
	request *structurev1.DeleteTableRequest,
) (*emptypb.Empty, error) {
	principal, _, err := service.managerPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if service.outbox == nil {
		return nil, deal.RPCError(deal.CodeServiceUnavailable)
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
		// H-33（票 04）：最后一张表禁删是业务状态约束，返回专有码 4003 而非 1001。
		return nil, deal.RPCError(deal.CodeLastTableProtected)
	}
	if !request.GetConfirm() {
		return &emptypb.Empty{}, nil
	}
	if deleter, ok := service.impact.(TableRecordsDeleter); ok {
		if err := deleter.DeleteTableRecords(ctx, table.ID); err != nil {
			return nil, err
		}
	}
	tx, err := service.repository.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := service.repository.DeleteTableTx(ctx, tx, table.ID); err != nil {
		return nil, mapStructureError(err)
	}
	event, err := tableDeletedEvent(table, principal.GetUserId(), service.now())
	if err != nil {
		return nil, err
	}
	if err := service.outbox.Enqueue(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("enqueue table.deleted: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit DeleteTable: %w", err)
	}
	return &emptypb.Empty{}, nil
}

// tableDeletedEvent 组装 table.deleted 事件；payload 即审计 detail_map 的来源。
func tableDeletedEvent(
	table base.StructureTable,
	operator string,
	occurredAt time.Time,
) (eventbus.Event, error) {
	payload, err := json.Marshal(map[string]any{
		"enterprise_id": table.EnterpriseID,
		"base_id":       table.BaseID,
		"table_id":      table.ID,
		"table_name":    table.Name,
		"operator":      operator,
	})
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("marshal table.deleted payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("generate table.deleted event id: %w", err)
	}
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "table.deleted",
			Version:    1,
			BaseID:     table.BaseID,
			OccurredAt: occurredAt.UnixMilli(),
			Producer:   "structure",
		},
		Payload: payload,
	}, nil
}
