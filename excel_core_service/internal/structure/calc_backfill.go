// structure/calc_backfill.go：结构域公式/引用字段写路径的回填入队（N5 缺陷）。
// 新建、复制、更改类型到公式或引用字段成功后，在自身事务内经事件总线落
// calc.job 回填种子作业：载荷含新作业 ID、表 ID 与新字段 ID 列表，不含触发
// 记录——calc 消费者对无触发记录作业按表分页全量物化，幂等键防重跑。
package structure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"excel-libs/eventbus"
	"excel-libs/idgen"

	"excel_core_service/base"
	"excel_core_service/deal"
)

// fieldNeedsCalcBackfill 报告字段类型是否需要回填作业：公式字段依赖
// excel_calc_materialized 权威投影（缺失即前端「计算中…」）；引用字段读路径
// 实时物化，入队作业保持三类计算字段回填口径一致（作业对引用字段为空转成功）。
// 关联字段的值是用户数据（链接 id），无物化列，不入队。
func fieldNeedsCalcBackfill(fieldType string) bool {
	return fieldType == "FIELD_TYPE_FORMULA" || fieldType == "FIELD_TYPE_LOOKUP"
}

// enqueueCalcBackfill 在结构写事务内落一个 calc.job 回填种子事件。outbox 未
// 装配按 9001 整体回滚（与 publishStructureChanged 同口径）：回填缺失即缺陷
// N5 的「计算中…」永久占位原样复现，不允许静默跳过。
func (service *Service) enqueueCalcBackfill(
	ctx context.Context,
	tx *sql.Tx,
	table base.StructureTable,
	fieldID string,
) error {
	if service.outbox == nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	jobID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate calc backfill job id: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"job_id":        jobID,
		"table_id":      table.ID,
		"field_id_list": []string{fieldID},
		"trigger": map[string]string{
			"record_id": "",
			"field_id":  "",
		},
		"backfill": map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("marshal calc backfill payload: %w", err)
	}
	eventID, err := idgen.New()
	if err != nil {
		return fmt.Errorf("generate calc backfill event id: %w", err)
	}
	return service.outbox.Enqueue(ctx, tx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "calc.job",
			Version:    1,
			BaseID:     table.BaseID,
			OccurredAt: service.now().UnixMilli(),
			Producer:   "structure",
		},
		Payload: payload,
	})
}

// cleanupFieldMaterializationTx 在结构写事务内清理一个字段的公式物化列
// （删除字段级联与转出公式/引用类型同口径）。calc 仓储未装配按 9001 整体
// 回滚：残留投影会让转类型后的读取口径不可预期，不允许静默跳过。
func (service *Service) cleanupFieldMaterializationTx(
	ctx context.Context,
	tx *sql.Tx,
	fieldID string,
) error {
	if service.calcRepository == nil {
		return deal.RPCError(deal.CodeServiceUnavailable)
	}
	return service.calcRepository.DeleteMaterializedByFieldTx(ctx, tx, fieldID)
}
