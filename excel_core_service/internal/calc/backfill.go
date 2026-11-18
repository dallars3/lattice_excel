// calc/backfill.go：无触发记录的回填作业（N5 缺陷）。结构域新建/复制/转类型
// 生成公式或引用字段后，经事件总线入队回填种子作业（载荷含 backfill 标记、
// 不含触发记录）；本文件把种子作业按记录页物化，并在同一事务内入队下一页
// 作业，直到全表物化完成。分批原则：单作业事务量可控。
package calc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"excel-libs/eventbus"
)

// backfillPageSize 是回填作业单批处理的记录数上限。决策依据（票 03 备注）：
// 单作业事务的行写入 ≈ 页内记录数 × 受影响公式字段数 × 2（excel_calc_materialized
// upsert + excel_bus_outbox record.changed 追加）。500 条 × 少量公式字段在
// MySQL 单事务写入量与锁持有时间内都留有充分余量，同时 10 万行表只需约 200 个
// 作业即可推进完成；页边界用 limit+1 探测，多取一条不影响正确性。
const backfillPageSize = 500

// pageBackfillRecords 无触发记录的回填作业按游标取下一页记录；返回本页记录
// 与「是否还有下一页」。游标为空表示从头开始。非回填作业不做分页。
func (service *Service) pageBackfillRecords(
	ctx context.Context,
	job calcJob,
) ([]string, bool, error) {
	if !job.Backfill {
		return nil, false, nil
	}
	pager, ok := service.materializer.(BackfillRecordPager)
	if !ok {
		return nil, false, errors.New("calc backfill job requires the record pager seam")
	}
	page, err := pager.RecordIDsAfter(ctx, job.TableID, job.BackfillAfterRecordID, backfillPageSize+1)
	if err != nil {
		return nil, false, fmt.Errorf(
			"page backfill records %s after %q: %w",
			job.TableID,
			job.BackfillAfterRecordID,
			err,
		)
	}
	hasMore := len(page) > backfillPageSize
	if hasMore {
		page = page[:backfillPageSize]
	}
	return page, hasMore, nil
}

// enqueueBackfillChain 在当前事务内入队下一页回填作业（游标=本页最后一条记录），
// 与本页物化原子提交：页写入成功则链不断，页回滚则链式事件一并撤销。
func (service *Service) enqueueBackfillChain(
	ctx context.Context,
	tx *sql.Tx,
	job calcJob,
	afterRecordID string,
) error {
	jobID, err := newULID(service.now())
	if err != nil {
		return fmt.Errorf("generate backfill chain job id: %w", err)
	}
	payload, err := json.Marshal(calcJobPayload{
		JobID:       jobID,
		TableID:     job.TableID,
		FieldIDList: job.FieldIDList,
		Backfill:    &calcJobBackfill{AfterRecordID: afterRecordID},
	})
	if err != nil {
		return fmt.Errorf("marshal backfill chain payload: %w", err)
	}
	eventID, err := newULID(service.now())
	if err != nil {
		return fmt.Errorf("generate backfill chain event id: %w", err)
	}
	return service.outbox.Enqueue(ctx, tx, eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    eventID,
			Type:       "calc.job",
			Version:    1,
			BaseID:     job.BaseID,
			OccurredAt: service.now().UnixMilli(),
			Producer:   "calc",
		},
		Payload: payload,
	})
}
