// structure/structure_changed.go：结构变更实时事件（W-11 结构广播）。
package structure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"excel-libs/eventbus"
	"excel-libs/idgen"

	"excel_core_service/base"
	"excel_core_service/deal"
)

// 结构变更类型：change_type 携带 field/table/view 词汇，realtime 侧据此把
// record.changed 事件转成 W-11 structure_changed 广播（含断线重放）。
const (
	structureChangeFieldCreated        = "field_created"
	structureChangeFieldRenamed        = "field_renamed"
	structureChangeFieldUpdated        = "field_updated"
	structureChangeFieldDuplicated     = "field_duplicated"
	structureChangeFieldConverted      = "field_converted"
	structureChangeFieldDeleted        = "field_deleted"
	structureChangeFieldOptionsUpdated = "field_options_updated"
	structureChangeTableCreated        = "table_created"
	structureChangeTableRenamed        = "table_renamed"
	structureChangeTableDeleted        = "table_deleted"
	structureChangeTableDuplicated     = "table_duplicated"
	structureChangeViewCreated         = "view_created"
	structureChangeViewRenamed         = "view_renamed"
	structureChangeViewUpdated         = "view_updated"
	structureChangeViewDeleted         = "view_deleted"
	structureChangeViewDuplicated      = "view_duplicated"
)

// structureChangedEvent 组装 record.changed 结构事件；payload.table_id 供
// records.GetEventsSince 按表过滤补发（断线重放不重不漏的前提）。
func structureChangedEvent(
	baseID, tableID, fieldID, changeType, operator string,
	occurredAt time.Time,
) (eventbus.Event, error) {
	payloadMap := map[string]any{
		"table_id":    tableID,
		"change_type": changeType,
		"operator":    operator,
	}
	if fieldID != "" {
		payloadMap["field_id"] = fieldID
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("marshal structure changed payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return eventbus.Event{}, fmt.Errorf("generate structure changed event id: %w", err)
	}
	return eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "record.changed",
			Version:    1,
			BaseID:     baseID,
			OccurredAt: occurredAt.UnixMilli(),
			Producer:   "structure",
		},
		Payload: payload,
	}, nil
}

// publishStructureChanged 在结构写事务内落 outbox：广播事件与结构写原子提交。
// 事件缺失即房间内协作者永久错过该结构变更且重放对不齐，因此 outbox 未装配或
// 落库失败按 9001 整体回滚（与 DeleteTable 的 table.deleted 同一口径）。
func (service *Service) publishStructureChanged(
	ctx context.Context,
	tx *sql.Tx,
	table base.StructureTable,
	fieldID, changeType, operator string,
) error {
	if service.outbox == nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	event, err := structureChangedEvent(
		table.BaseID,
		table.ID,
		fieldID,
		changeType,
		operator,
		service.now(),
	)
	if err != nil {
		return err
	}
	return service.outbox.Enqueue(ctx, tx, event)
}
