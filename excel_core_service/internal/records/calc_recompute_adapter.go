// records/calc_recompute_adapter.go：一文件一接口。calc 消费者的 records 侧
// 适配器——权威物化写入（calc 作业事务随后以同一计算值 upsert 覆盖，双重写入
// 幂等一致）、公式求值行上下文（字段 id 键投影为字段名键）、关联行解析与
// 回填分页（N5 无触发记录作业）。生产装配见 cmd/main.go。
package records

import (
	"context"
	"errors"
	"fmt"

	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
)

// CalcRecomputeAdapter adapts records persistence to the calc module's
// recompute seams（CellMaterializer / RowContextReader / RelationRecordFinder /
// BackfillRecordPager）.
type CalcRecomputeAdapter struct {
	records   *base.RecordsRepository
	structure *base.StructureRepository
	calc      *base.CalcRepository
}

// NewCalcRecomputeAdapter creates the adapter wired into the calc service.
func NewCalcRecomputeAdapter(
	recordsRepository *base.RecordsRepository,
	structureRepository *base.StructureRepository,
	calcRepository *base.CalcRepository,
) *CalcRecomputeAdapter {
	return &CalcRecomputeAdapter{
		records:   recordsRepository,
		structure: structureRepository,
		calc:      calcRepository,
	}
}

// WriteComputed 是消费链路的写通知，不自行落库：processCalcJob 对每个计算格
// 在作业事务内 SaveMaterialized（权威持久化，与 record.changed 广播和作业
// 收尾原子提交）。本接缝若提前落库，会把「物化行可见」提到作业提交之前——
// 与删除字段级联/转出清理产生 resurrect 竞态（N5 票 03）。如需扩展
// cells_json 投影在此挂接，持久化仍必须走作业事务。
func (adapter *CalcRecomputeAdapter) WriteComputed(
	ctx context.Context,
	recordID, fieldID string,
	value *commonv1.Value,
) error {
	_, _, _ = recordID, fieldID, value
	return nil
}

// ReadRowContext reads one active record's cells projected from field-id keys
// to field-name keys（公式表达式按字段名引用）。结构中已不存在的孤儿键跳过。
func (adapter *CalcRecomputeAdapter) ReadRowContext(
	ctx context.Context,
	tableID, recordID string,
) (map[string]*commonv1.Value, error) {
	if adapter == nil || adapter.records == nil || adapter.structure == nil {
		return nil, errors.New("records and structure repositories are required")
	}
	record, err := adapter.records.RecordByID(ctx, recordID)
	if err != nil {
		return nil, fmt.Errorf("read row context %s/%s: %w", tableID, recordID, err)
	}
	if record.TableID != tableID {
		return nil, fmt.Errorf(
			"read row context %s/%s: record belongs to table %s",
			tableID,
			recordID,
			record.TableID,
		)
	}
	fields, err := adapter.structure.FieldsByTable(ctx, tableID)
	if err != nil {
		return nil, fmt.Errorf("read row context %s/%s: %w", tableID, recordID, err)
	}
	names := make(map[string]string, len(fields))
	for _, field := range fields {
		names[field.ID] = field.Name
	}
	row := make(map[string]*commonv1.Value, len(record.Cells))
	for fieldID, value := range record.Cells {
		name, ok := names[fieldID]
		if !ok || isClearedCell(value) {
			continue
		}
		row[name] = value
	}
	return row, nil
}

// RowsReferencing resolves the active rows whose relation field value contains
// targetRecordID（跨表重算的受影响行集来源）。
func (adapter *CalcRecomputeAdapter) RowsReferencing(
	ctx context.Context,
	tableID, fieldID, targetRecordID string,
) ([]string, error) {
	if adapter == nil || adapter.records == nil {
		return nil, errors.New("records repository is required")
	}
	return adapter.records.RecordIDsReferencing(ctx, tableID, fieldID, targetRecordID)
}

// RecordIDsAfter pages a table's active record ids for trigger-less backfill
// jobs（N5）；游标为上一页最后一条记录 id，空串从表头开始。
func (adapter *CalcRecomputeAdapter) RecordIDsAfter(
	ctx context.Context,
	tableID, afterRecordID string,
	limit int,
) ([]string, error) {
	if adapter == nil || adapter.records == nil {
		return nil, errors.New("records repository is required")
	}
	return adapter.records.RecordIDsAfter(ctx, tableID, afterRecordID, limit)
}
