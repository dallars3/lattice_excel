// records/structure_impact_test.go：structure 模块 impact 缝的 records 侧适配器
// 契约——单一适配器同时满足转换预览/确认迁移与表记录复制（工单04），且不把
// DuplicateTableRecords 挂到 FieldConversionAdapter 上（转换路径不得携带复制行为）。
package records_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	commonv1 "excel/api/common/v1"

	"excel_core_service/base"
	"excel_core_service/internal/records"
	"excel_core_service/internal/structure"
)

const (
	impactSourceTableID = "01J00000000000000000004001"
	impactTargetTableID = "01J00000000000000000004002"
	impactTextFieldID   = "01J00000000000000000004003"
	impactRecordID      = "01J00000000000000000004004"
	impactTargetTextID  = "01J00000000000000000004005"
	impactOperatorID    = "01J00000000000000000004006"
	impactCreatorID     = "01J00000000000000000004007"
	impactEnterpriseID  = "01J00000000000000000000904"
	impactBaseID        = "01J00000000000000000004901"
)

// TestStructureImpactAdapterServesStructureSeams locks the seam contract: the
// adapter satisfies every structure impact interface, and record copying stays
// reachable through the dedicated adapter instead of the conversion adapter.
func TestStructureImpactAdapterServesStructureSeams(t *testing.T) {
	adapter := records.NewStructureImpactAdapter(nil, nil, nil, nil)

	var _ structure.RecordsImpactCounter = adapter
	var _ structure.FieldConversionImpactCounter = adapter
	var _ structure.FieldConversionApplier = adapter
	var _ structure.TableRecordsDuplicator = adapter
}

// TestStructureImpactAdapterDuplicateTableRecordsCopiesRows 行为基线：经适配器
// 复制表记录后行值保留、版本重置、created_by/created_at 按新记录语义生成。
func TestStructureImpactAdapterDuplicateTableRecordsCopiesRows(t *testing.T) {
	db := openImpactFixtureDB(t)
	t.Cleanup(func() { deleteImpactFixtureRows(t, db) })
	seedImpactFixture(t, db)

	adapter := records.NewStructureImpactAdapter(
		base.NewRecordsRepository(db),
		base.NewStructureRepository(db),
		base.NewCalcRepository(db),
		nil,
	)
	ctx := incomingPrincipalContext(impactOperatorID)
	if err := adapter.DuplicateTableRecords(ctx, impactSourceTableID, impactTargetTableID); err != nil {
		t.Fatalf("DuplicateTableRecords() error = %v", err)
	}

	copied, err := base.NewRecordsRepository(db).RecordsByTable(
		context.Background(),
		impactTargetTableID,
		"",
	)
	if err != nil {
		t.Fatalf("RecordsByTable(copy) error = %v", err)
	}
	if len(copied) != 1 {
		t.Fatalf("copied records = %d, want 1", len(copied))
	}
	record := copied[0]
	if record.ID == impactRecordID {
		t.Fatal("copied record reused the source record id")
	}
	if got := record.Cells[impactTargetTextID].GetStringValue(); got != "盘点行t04" {
		t.Fatalf("copied cell = %q, want 盘点行t04", got)
	}
	if record.Version != 1 {
		t.Fatalf("copied version = %d, want 1 (new-record semantics)", record.Version)
	}
	if record.CreatedBy != impactOperatorID {
		t.Fatalf("copied created_by = %q, want operator %q", record.CreatedBy, impactOperatorID)
	}
	if record.CreatedAt < time.Now().Add(-time.Minute).UnixMilli() ||
		record.CreatedAt > time.Now().Add(time.Minute).UnixMilli() {
		t.Fatalf("copied created_at = %d, want current time", record.CreatedAt)
	}
}

// TestStructureImpactAdapterAffectedRecordsStillCounts 转换预览缝不回归：同一
// 适配器仍统计受影响记录数（缺陷 #26 既有行为）。
func TestStructureImpactAdapterAffectedRecordsStillCounts(t *testing.T) {
	db := openImpactFixtureDB(t)
	t.Cleanup(func() { deleteImpactFixtureRows(t, db) })
	seedImpactFixture(t, db)

	adapter := records.NewStructureImpactAdapter(
		base.NewRecordsRepository(db),
		base.NewStructureRepository(db),
		base.NewCalcRepository(db),
		nil,
	)
	affected, err := adapter.AffectedRecords(context.Background(), impactSourceTableID, impactTextFieldID)
	if err != nil {
		t.Fatalf("AffectedRecords() error = %v", err)
	}
	if affected != 1 {
		t.Fatalf("AffectedRecords() = %d, want 1", affected)
	}
}

func openImpactFixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := base.OpenDB(recordsTestMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	deleteImpactFixtureRows(t, db)
	return db
}

// seedImpactFixture 落一对源/副本表结构（字段名一致，与 structure.DuplicateTable
// 的结构复制产物一致）与一条源记录。
func seedImpactFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	structureRepository := base.NewStructureRepository(db)
	textConfig, err := base.EncodeStructureConfig(map[string]any{})
	if err != nil {
		t.Fatalf("encode text config: %v", err)
	}
	for _, table := range []base.StructureTable{
		{ID: impactSourceTableID, EnterpriseID: impactEnterpriseID, BaseID: impactBaseID, Name: "盘点表t04", CreatedAt: now},
		{ID: impactTargetTableID, EnterpriseID: impactEnterpriseID, BaseID: impactBaseID, Name: "盘点表t04 的副本", CreatedAt: now},
	} {
		if err := structureRepository.CreateTable(context.Background(), table); err != nil {
			t.Fatalf("seed table %s: %v", table.ID, err)
		}
	}
	for index, field := range []base.StructureField{
		{ID: impactTextFieldID, TableID: impactSourceTableID, Name: "标题", Type: "FIELD_TYPE_TEXT", Config: textConfig, IsPrimary: true, CreatedAt: now},
		{ID: impactTargetTextID, TableID: impactTargetTableID, Name: "标题", Type: "FIELD_TYPE_TEXT", Config: textConfig, IsPrimary: true, CreatedAt: now},
	} {
		field.Position = index
		if err := structureRepository.CreateField(context.Background(), field, nil); err != nil {
			t.Fatalf("seed field %s: %v", field.ID, err)
		}
	}
	repository := base.NewRecordsRepository(db)
	tx, err := repository.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("records BeginTx() error = %v", err)
	}
	record := base.Record{
		ID:        impactRecordID,
		TableID:   impactSourceTableID,
		Cells: map[string]*commonv1.Value{
			impactTextFieldID: {Kind: &commonv1.Value_StringValue{StringValue: "盘点行t04"}},
		},
		CreatedBy: impactCreatorID,
		CreatedAt: now,
		Version:   5,
	}
	if err := repository.CreateRecordTx(context.Background(), tx, record); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seeded record: %v", err)
	}
}

func deleteImpactFixtureRows(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{query: `DELETE FROM excel_calc_materialized WHERE record_id IN (SELECT id FROM excel_records WHERE table_id IN (?, ?))`,
			args: []any{impactSourceTableID, impactTargetTableID}},
		{query: `DELETE FROM excel_records_histories WHERE record_id IN (SELECT id FROM excel_records WHERE table_id IN (?, ?))`,
			args: []any{impactSourceTableID, impactTargetTableID}},
		{query: `DELETE FROM excel_records WHERE table_id IN (?, ?)`,
			args: []any{impactSourceTableID, impactTargetTableID}},
		{query: `DELETE FROM excel_structure_fields WHERE table_id IN (?, ?)`,
			args: []any{impactSourceTableID, impactTargetTableID}},
		{query: `DELETE FROM excel_structure_views WHERE table_id IN (?, ?)`,
			args: []any{impactSourceTableID, impactTargetTableID}},
		{query: `DELETE FROM excel_structure_tables WHERE id IN (?, ?)`,
			args: []any{impactSourceTableID, impactTargetTableID}},
	} {
		if _, err := db.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatalf("delete impact fixture rows with %q: %v", statement.query, err)
		}
	}
}
