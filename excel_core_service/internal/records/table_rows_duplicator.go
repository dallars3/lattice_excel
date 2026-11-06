// records/table_rows_duplicator.go：表记录行复制适配。复制 Base（workspace
// 工单03）与复制数据表（structure 工单04）共用的可复用缝：按字段名把源表字段
// 映射到副本字段，保留字段值重映射单元格键，关系值在同被复制时改写到副本记录，
// 公式物化值随行迁移；版本重置为新记录语义（version=1），created_by/created_at
// 取复制操作者与当下。评论、历史、oplog 不复制。
package records

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"excel-libs/idgen"
	commonv1 "excel/api/common/v1"

	"excel_core_service/base"
	"excel_core_service/deal"
)

// rowsCopyChunk 是物化行加载的批大小，避免超大 IN 列表。
const rowsCopyChunk = 500

// TableRowsDuplicator adapts the records persistence to the row-copy seams of
// workspace (whole duplicated Base) and structure (one copied table).
type TableRowsDuplicator struct {
	records   *base.RecordsRepository
	structure *base.StructureRepository
	calc      *base.CalcRepository
	now       func() time.Time
}

// NewTableRowsDuplicator creates the records row-copy adapter; now 为空时取
// time.Now（复制流程显式传入以对齐服务时钟）。
func NewTableRowsDuplicator(
	recordsRepository *base.RecordsRepository,
	structureRepository *base.StructureRepository,
	calcRepository *base.CalcRepository,
	now func() time.Time,
) *TableRowsDuplicator {
	if now == nil {
		now = time.Now
	}
	return &TableRowsDuplicator{
		records:   recordsRepository,
		structure: structureRepository,
		calc:      calcRepository,
		now:       now,
	}
}

// DuplicateBaseRows copies every source table's rows into their copied targets
// in one transaction: 任一表失败即整体回滚，副本 Base 不留半截记录。tableIDs
// 是结构复制返回的 源表id→目标表id 映射。
func (duplicator *TableRowsDuplicator) DuplicateBaseRows(
	ctx context.Context,
	tableIDs map[string]string,
) error {
	if duplicator == nil || duplicator.records == nil ||
		duplicator.structure == nil || duplicator.calc == nil {
		return errors.New("records row duplicator is not configured")
	}
	if len(tableIDs) == 0 {
		return nil
	}
	plans := make([]rowsTablePlan, 0, len(tableIDs))
	for sourceTableID, targetTableID := range tableIDs {
		plan, err := duplicator.buildTablePlan(ctx, sourceTableID, targetTableID)
		if err != nil {
			return err
		}
		plans = append(plans, plan)
	}
	return duplicator.copyInNewTx(ctx, plans)
}

// DuplicateTableRows copies one table's rows into its freshly copied table in
// one transaction；关系值只在指向本表（自关系）时改写到副本记录，指向其它表
// 的关系保持原记录引用。这是复制数据表（工单04）复用的单表缝。
func (duplicator *TableRowsDuplicator) DuplicateTableRows(
	ctx context.Context,
	sourceTableID, targetTableID string,
) error {
	if duplicator == nil || duplicator.records == nil ||
		duplicator.structure == nil || duplicator.calc == nil {
		return errors.New("records row duplicator is not configured")
	}
	if sourceTableID == "" || targetTableID == "" {
		return deal.RPCError(deal.CodeInvalidArgument)
	}
	plan, err := duplicator.buildTablePlan(ctx, sourceTableID, targetTableID)
	if err != nil {
		return err
	}
	return duplicator.copyInNewTx(ctx, []rowsTablePlan{plan})
}

func (duplicator *TableRowsDuplicator) copyInNewTx(
	ctx context.Context,
	plans []rowsTablePlan,
) error {
	operator, err := duplicationOperator(ctx)
	if err != nil {
		return err
	}
	tx, err := duplicator.records.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := duplicator.copyRowsTx(ctx, tx, plans, operator, duplicator.now()); err != nil {
		return err
	}
	return tx.Commit()
}

// rowsTablePlan 是一对源/目标数据表及其字段映射；relationTargets 记录源关系
// 字段 id→源目标表 id，用于判断关系值是否需要改写到副本记录。
type rowsTablePlan struct {
	sourceTableID   string
	targetTableID   string
	fieldIDs        map[string]string
	relationTargets map[string]string
}

type sourceTableRow struct {
	plan   rowsTablePlan
	record base.Record
}

// buildTablePlan 按字段名对齐源/副本字段（结构复制保留字段名且表内重名被
// 1002 拒绝，名字匹配无歧义）；缺失或类型不一致视为结构复制不完整并失败。
func (duplicator *TableRowsDuplicator) buildTablePlan(
	ctx context.Context,
	sourceTableID, targetTableID string,
) (rowsTablePlan, error) {
	sourceFields, err := duplicator.structure.FieldsByTable(ctx, sourceTableID)
	if err != nil {
		return rowsTablePlan{}, fmt.Errorf("load source table %s fields: %w", sourceTableID, err)
	}
	targetFields, err := duplicator.structure.FieldsByTable(ctx, targetTableID)
	if err != nil {
		return rowsTablePlan{}, fmt.Errorf("load copied table %s fields: %w", targetTableID, err)
	}
	targetByName := make(map[string]base.StructureField, len(targetFields))
	for _, field := range targetFields {
		if _, exists := targetByName[field.Name]; exists {
			return rowsTablePlan{}, fmt.Errorf("copied table %s field name %q is ambiguous", targetTableID, field.Name)
		}
		targetByName[field.Name] = field
	}
	plan := rowsTablePlan{
		sourceTableID:   sourceTableID,
		targetTableID:   targetTableID,
		fieldIDs:        make(map[string]string, len(sourceFields)),
		relationTargets: make(map[string]string),
	}
	for _, field := range sourceFields {
		target, ok := targetByName[field.Name]
		if !ok {
			return rowsTablePlan{}, fmt.Errorf("copied table %s misses field %q", targetTableID, field.Name)
		}
		if target.Type != field.Type {
			return rowsTablePlan{}, fmt.Errorf("copied field %q type = %s, want %s", field.Name, target.Type, field.Type)
		}
		plan.fieldIDs[field.ID] = target.ID
		if field.Type == "FIELD_TYPE_RELATION" {
			config, err := base.DecodeStructureConfig(field.Config)
			if err != nil {
				return rowsTablePlan{}, fmt.Errorf("decode relation field %q config: %w", field.Name, err)
			}
			if targetTableID, ok := config["target_table_id"].(string); ok && targetTableID != "" {
				plan.relationTargets[field.ID] = targetTableID
			}
		}
	}
	return plan, nil
}

// copyRowsTx 两段式复制：先为全部源记录分配副本 id（跨表关系值改写需要完整
// 映射），再重映射单元格落库并随行迁移公式物化值。整段在调用者事务内执行。
func (duplicator *TableRowsDuplicator) copyRowsTx(
	ctx context.Context,
	tx *sql.Tx,
	plans []rowsTablePlan,
	operator string,
	now time.Time,
) error {
	copiedTables := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		copiedTables[plan.sourceTableID] = struct{}{}
	}
	rows := make([]sourceTableRow, 0)
	for _, plan := range plans {
		sourceRecords, err := duplicator.records.RecordsByTableTx(ctx, tx, plan.sourceTableID)
		if err != nil {
			return fmt.Errorf("load source table %s rows: %w", plan.sourceTableID, err)
		}
		for _, record := range sourceRecords {
			rows = append(rows, sourceTableRow{plan: plan, record: record})
		}
	}
	recordIDs := make(map[string]string, len(rows))
	for _, row := range rows {
		recordID, err := idgen.New()
		if err != nil {
			return err
		}
		recordIDs[row.record.ID] = recordID
	}
	for _, row := range rows {
		record := base.Record{
			ID:        recordIDs[row.record.ID],
			TableID:   row.plan.targetTableID,
			Cells:     remapRecordCells(row.record.Cells, row.plan, copiedTables, recordIDs),
			CreatedBy: operator,
			CreatedAt: now.UnixMilli(),
			Version:   1,
		}
		if err := duplicator.records.CreateRecordTx(ctx, tx, record); err != nil {
			return err
		}
	}
	return duplicator.copyMaterializedTx(ctx, tx, rows, recordIDs)
}

// remapRecordCells 重映射一个源记录的单元格：键换成目标字段 id；关系值在关系
// 目标表同被复制时按记录映射改写；结构已不存在的孤儿单元格不复制。
func remapRecordCells(
	cells map[string]*commonv1.Value,
	plan rowsTablePlan,
	copiedTables map[string]struct{},
	recordIDs map[string]string,
) map[string]*commonv1.Value {
	remapped := make(map[string]*commonv1.Value, len(cells))
	for sourceFieldID, value := range cells {
		targetFieldID, ok := plan.fieldIDs[sourceFieldID]
		if !ok {
			continue
		}
		if targetTableID, isRelation := plan.relationTargets[sourceFieldID]; isRelation {
			if _, copied := copiedTables[targetTableID]; copied {
				value = remapRecordReferences(value, recordIDs)
			}
		}
		remapped[targetFieldID] = value
	}
	return remapped
}

// remapRecordReferences 把关系单元格里的记录 id 改写为副本 id；列表逐项改写，
// 未映射的引用（如已软删的源记录）保持原值，读回时与源一致渲染为空。
func remapRecordReferences(
	value *commonv1.Value,
	recordIDs map[string]string,
) *commonv1.Value {
	switch kind := value.GetKind().(type) {
	case *commonv1.Value_StringValue:
		if mapped, ok := recordIDs[kind.StringValue]; ok {
			return &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: mapped}}
		}
		return value
	case *commonv1.Value_ListValue:
		items := make([]*commonv1.Value, 0, len(kind.ListValue.GetValueList()))
		changed := false
		for _, item := range kind.ListValue.GetValueList() {
			if mapped, ok := recordIDs[item.GetStringValue()]; ok {
				items = append(items, &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: mapped}})
				changed = true
				continue
			}
			items = append(items, item)
		}
		if !changed {
			return value
		}
		return &commonv1.Value{Kind: &commonv1.Value_ListValue{
			ListValue: &commonv1.ValueList{ValueList: items},
		}}
	default:
		return value
	}
}

// copyMaterializedTx 迁移公式物化值（excel_calc_materialized）：行键从源
// record/field 换到副本，值、哨兵状态、原因与 formula_hash 原样保留。
// 跨模块 SQL（§2.5）：公式读投影与记录同库，不随行迁移则副本公式格读回全部
// 渲染为空，与「副本数据与源一致」相悖，故在记录复制事务内一并搬运。
func (duplicator *TableRowsDuplicator) copyMaterializedTx(
	ctx context.Context,
	tx *sql.Tx,
	rows []sourceTableRow,
	recordIDs map[string]string,
) error {
	if len(rows) == 0 {
		return nil
	}
	rowsByTable := make(map[string][]sourceTableRow, len(rows))
	for _, row := range rows {
		rowsByTable[row.plan.sourceTableID] = append(rowsByTable[row.plan.sourceTableID], row)
	}
	for _, tableRows := range rowsByTable {
		plan := tableRows[0].plan
		for start := 0; start < len(tableRows); start += rowsCopyChunk {
			end := start + rowsCopyChunk
			if end > len(tableRows) {
				end = len(tableRows)
			}
			chunk := tableRows[start:end]
			sourceIDs := make([]string, 0, len(chunk))
			for _, row := range chunk {
				sourceIDs = append(sourceIDs, row.record.ID)
			}
			materialized, err := duplicator.calc.CalcMaterializedByRecordIDs(ctx, tx, sourceIDs)
			if err != nil {
				return fmt.Errorf("load calc materialized for table %s: %w", plan.sourceTableID, err)
			}
			for _, item := range materialized {
				targetFieldID, ok := plan.fieldIDs[item.FieldID]
				if !ok {
					continue
				}
				if err := duplicator.calc.SaveMaterialized(ctx, tx, base.CalcMaterializedRecord{
					RecordID:    recordIDs[item.RecordID],
					FieldID:     targetFieldID,
					ValueJSON:   item.ValueJSON,
					FormulaHash: item.FormulaHash,
					Status:      item.Status,
					Reason:      item.Reason,
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// duplicationOperator 解析复制操作者：created_by 按新记录语义生成，必须来自
// 请求主体；复制流程均在带身份的 RPC 内执行，缺失即失败。
func duplicationOperator(ctx context.Context) (string, error) {
	principal, err := deal.PrincipalFromContext(ctx)
	if err != nil {
		return "", err
	}
	if principal.GetUserId() == "" {
		return "", deal.RPCError(deal.CodeInvalidArgument)
	}
	return principal.GetUserId(), nil
}
