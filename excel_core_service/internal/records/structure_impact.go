// records/structure_impact.go：structure 模块 impact 缝的 records 侧组合适配器。
// structure.Service 只持有一个 impact 字段：字段转换预览/确认迁移
// （FieldConversionAdapter）与复制数据表的记录复制（TableRowsDuplicator 的单表
// 缝，工单04）由此适配器一并满足。复制方法不挂在 FieldConversionAdapter 上，
// 转换路径不会意外携带复制行为。
package records

import (
	"context"
	"time"

	"excel_core_service/base"
)

// StructureImpactAdapter composes the records seams consumed by the structure
// module's single impact field.
type StructureImpactAdapter struct {
	*FieldConversionAdapter
	rows *TableRowsDuplicator
}

// NewStructureImpactAdapter creates the combined structure impact adapter.
func NewStructureImpactAdapter(
	recordsRepository *base.RecordsRepository,
	structureRepository *base.StructureRepository,
	calcRepository *base.CalcRepository,
	now func() time.Time,
) *StructureImpactAdapter {
	return &StructureImpactAdapter{
		FieldConversionAdapter: NewFieldConversionAdapter(recordsRepository, structureRepository),
		rows:                   NewTableRowsDuplicator(recordsRepository, structureRepository, calcRepository, now),
	}
}

// DuplicateTableRecords copies one table's rows into its freshly copied table:
// 字段值按字段名保留，自关系值改写到副本记录，公式物化值随行迁移；版本重置为
// 新记录语义（version=1），created_by/created_at 取复制操作者与当下。评论、
// 历史与 oplog 不复制。
func (adapter *StructureImpactAdapter) DuplicateTableRecords(
	ctx context.Context,
	sourceTableID, targetTableID string,
) error {
	return adapter.rows.DuplicateTableRows(ctx, sourceTableID, targetTableID)
}
