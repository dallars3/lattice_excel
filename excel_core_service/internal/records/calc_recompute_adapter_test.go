// records/calc_recompute_adapter_test.go：calc 消费者的 records 侧适配器
// （N5 回填 + 跨表重算的生产接缝）：分页列记录、行上下文按字段名投影、
// 关联行解析与物化写入（含事务感知）。
package records_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	commonv1 "excel/api/common/v1"
	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/internal/records"
)

const (
	calcAdapterBaseID   = "01J00000000000000000013001"
	calcAdapterTableID  = "01J00000000000000000013002"
	calcAdapterUserID   = "01J00000000000000000013003"
	calcAdapterSourceID = "01J00000000000000000013004"
	calcAdapterLabelID  = "01J00000000000000000013005"
	calcAdapterRelID    = "01J00000000000000000013006"
)

// calcAdapterRecordID 生成 26 字符的定长记录 id（前缀 23 + 3 位序号）。
func calcAdapterRecordID(index int) string {
	return fmt.Sprintf("01JCK000000000000000000%03d", index)
}

func newCalcAdapterHarness(t *testing.T) (*sql.DB, records.CalcRecomputeAdapter) {
	t.Helper()
	db := testmysql.OpenTestDB(t, recordsTestMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetWriteData(t, db)
	t.Cleanup(func() { resetWriteData(t, db) })

	structureRepo := base.NewStructureRepository(db)
	recordsRepo := base.NewRecordsRepository(db)
	calcRepo := base.NewCalcRepository(db)
	seedStructureField(t, db, calcAdapterSourceID, calcAdapterTableID, "Source", "FIELD_TYPE_NUMBER", 0)
	seedStructureField(t, db, calcAdapterLabelID, calcAdapterTableID, "Label", "FIELD_TYPE_TEXT", 1)
	seedStructureField(t, db, calcAdapterRelID, calcAdapterTableID, "Rel", "FIELD_TYPE_RELATION", 2)
	t.Cleanup(func() { resetCalcAdapterStructure(t, db) })
	return db, *records.NewCalcRecomputeAdapter(recordsRepo, structureRepo, calcRepo)
}

// resetCalcAdapterStructure 清理本夹具自建的结构字段，不触碰其他套件的数据。
func resetCalcAdapterStructure(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		DELETE FROM excel_structure_fields WHERE table_id = ?
	`, calcAdapterTableID); err != nil {
		t.Fatalf("reset calc adapter structure: %v", err)
	}
}

func seedStructureField(
	t *testing.T,
	db *sql.DB,
	fieldID, tableID, name, fieldType string,
	position int,
) {
	t.Helper()
	config, err := base.EncodeStructureConfig(map[string]any{})
	if err != nil {
		t.Fatalf("encode field config: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_structure_fields (
			id, table_id, name, type, config, is_primary, position, created_at
		) VALUES (?, ?, ?, ?, ?, 0, ?, 1)
	`, fieldID, tableID, name, fieldType, config, position); err != nil {
		t.Fatalf("seed structure field %s: %v", fieldID, err)
	}
}

// TestCalcRecomputeAdapterRecordIDsAfterPagesActiveRecords：分页按 id 升序、
// 游标推进、limit 截断、软删记录不参与回填。
func TestCalcRecomputeAdapterRecordIDsAfterPagesActiveRecords(t *testing.T) {
	ctx := context.Background()
	db, adapter := newCalcAdapterHarness(t)

	const total = 5
	for index := 0; index < total; index++ {
		seedRecord(t, db, calcAdapterRecordID(index), calcAdapterTableID, calcAdapterUserID, 1, nil)
	}
	// 软删一条（id 序在中间），分页必须跳过。
	if _, err := db.ExecContext(ctx, `
		UPDATE excel_records SET soft_deleted_at = 1 WHERE id = ?
	`, calcAdapterRecordID(2)); err != nil {
		t.Fatalf("soft delete record: %v", err)
	}

	first, err := adapter.RecordIDsAfter(ctx, calcAdapterTableID, "", 2)
	if err != nil {
		t.Fatalf("RecordIDsAfter(first) error = %v", err)
	}
	if len(first) != 2 || first[0] != calcAdapterRecordID(0) || first[1] != calcAdapterRecordID(1) {
		t.Fatalf("first page = %v, want records 0,1", first)
	}

	second, err := adapter.RecordIDsAfter(ctx, calcAdapterTableID, first[1], 2)
	if err != nil {
		t.Fatalf("RecordIDsAfter(second) error = %v", err)
	}
	if len(second) != 2 || second[0] != calcAdapterRecordID(3) || second[1] != calcAdapterRecordID(4) {
		t.Fatalf("second page = %v, want records 3,4 (soft-deleted 2 skipped)", second)
	}

	last, err := adapter.RecordIDsAfter(ctx, calcAdapterTableID, second[1], 2)
	if err != nil {
		t.Fatalf("RecordIDsAfter(last) error = %v", err)
	}
	if len(last) != 0 {
		t.Fatalf("last page = %v, want empty", last)
	}
}

// TestCalcRecomputeAdapterReadRowContextMapsFieldNames：行上下文把 cells 的
// 字段 id 键投影成字段名键（公式表达式按名引用）；结构中已不存在的孤儿键跳过。
func TestCalcRecomputeAdapterReadRowContextMapsFieldNames(t *testing.T) {
	ctx := context.Background()
	db, adapter := newCalcAdapterHarness(t)

	recordID := calcAdapterRecordID(0)
	seedRecord(t, db, recordID, calcAdapterTableID, calcAdapterUserID, 1, map[string]*commonv1.Value{
		calcAdapterSourceID: {Kind: &commonv1.Value_IntValue{IntValue: 2}},
		calcAdapterLabelID:  {Kind: &commonv1.Value_StringValue{StringValue: "文本"}},
		"01J00000000000000000013fff": {Kind: &commonv1.Value_StringValue{StringValue: "孤儿"}},
	})

	row, err := adapter.ReadRowContext(ctx, calcAdapterTableID, recordID)
	if err != nil {
		t.Fatalf("ReadRowContext() error = %v", err)
	}
	if len(row) != 2 {
		t.Fatalf("row keys = %d (%v), want 2 named fields", len(row), row)
	}
	if got := row["Source"].GetIntValue(); got != 2 {
		t.Fatalf("row[Source] = %d, want 2", got)
	}
	if got := row["Label"].GetStringValue(); got != "文本" {
		t.Fatalf("row[Label] = %q, want 文本", got)
	}

	if _, err := adapter.ReadRowContext(ctx, calcAdapterTableID, "01J00000000000000000013fff"); err == nil {
		t.Fatal("ReadRowContext(missing record) error = nil, want not-found error")
	}
}

// TestCalcRecomputeAdapterRowsReferencing：关联行解析覆盖单值与列表值，
// 软删记录不参与。
func TestCalcRecomputeAdapterRowsReferencing(t *testing.T) {
	ctx := context.Background()
	db, adapter := newCalcAdapterHarness(t)

	single := calcAdapterRecordID(0)
	list := calcAdapterRecordID(1)
	other := calcAdapterRecordID(2)
	seedRecord(t, db, single, calcAdapterTableID, calcAdapterUserID, 1, map[string]*commonv1.Value{
		calcAdapterRelID: {Kind: &commonv1.Value_StringValue{StringValue: "target-1"}},
	})
	seedRecord(t, db, list, calcAdapterTableID, calcAdapterUserID, 1, map[string]*commonv1.Value{
		calcAdapterRelID: {Kind: &commonv1.Value_ListValue{ListValue: &commonv1.ValueList{ValueList: []*commonv1.Value{
			{Kind: &commonv1.Value_StringValue{StringValue: "target-1"}},
			{Kind: &commonv1.Value_StringValue{StringValue: "target-2"}},
		}}}},
	})
	seedRecord(t, db, other, calcAdapterTableID, calcAdapterUserID, 1, map[string]*commonv1.Value{
		calcAdapterRelID: {Kind: &commonv1.Value_StringValue{StringValue: "target-3"}},
	})
	if _, err := db.ExecContext(ctx, `
		UPDATE excel_records SET soft_deleted_at = 1 WHERE id = ?
	`, single); err != nil {
		t.Fatalf("soft delete record: %v", err)
	}

	target1, err := adapter.RowsReferencing(ctx, calcAdapterTableID, calcAdapterRelID, "target-1")
	if err != nil {
		t.Fatalf("RowsReferencing(target-1) error = %v", err)
	}
	if len(target1) != 1 || target1[0] != list {
		t.Fatalf("rows referencing target-1 = %v, want [%s] (soft-deleted skipped)", target1, list)
	}
	target2, err := adapter.RowsReferencing(ctx, calcAdapterTableID, calcAdapterRelID, "target-2")
	if err != nil {
		t.Fatalf("RowsReferencing(target-2) error = %v", err)
	}
	if len(target2) != 1 || target2[0] != list {
		t.Fatalf("rows referencing target-2 = %v, want [%s]", target2, list)
	}
	none, err := adapter.RowsReferencing(ctx, calcAdapterTableID, calcAdapterRelID, "target-9")
	if err != nil {
		t.Fatalf("RowsReferencing(target-9) error = %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("rows referencing target-9 = %v, want none", none)
	}
}

// TestCalcRecomputeAdapterWriteComputedDefersToJobTransaction：WriteComputed
// 是消费链路写通知，不自行落库——物化行可见性必须与作业提交原子（提前落库
// 会与删除字段级联/转出清理产生 resurrect 竞态，N5 票 03）。
func TestCalcRecomputeAdapterWriteComputedDefersToJobTransaction(t *testing.T) {
	ctx := context.Background()
	db, adapter := newCalcAdapterHarness(t)

	recordID := calcAdapterRecordID(0)
	seedRecord(t, db, recordID, calcAdapterTableID, calcAdapterUserID, 1, nil)
	value := &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: 4}}

	if err := adapter.WriteComputed(ctx, recordID, calcAdapterSourceID, value); err != nil {
		t.Fatalf("WriteComputed() error = %v", err)
	}
	if _, ok, err := base.NewCalcRepository(db).Value(ctx, recordID, calcAdapterSourceID); err != nil {
		t.Fatalf("CalcRepository.Value() error = %v", err)
	} else if ok {
		t.Fatal("adapter persisted a materialized row outside the calc job transaction")
	}
}
