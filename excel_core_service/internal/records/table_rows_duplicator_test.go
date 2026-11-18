// records/table_rows_duplicator_test.go：单表行复制缝契约——复制数据表
// （structure 工单04）复用此能力时的行为基线。
package records_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"

	commonv1 "excel/api/common/v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"excel-libs/testmysql"
	"excel_core_service/base"
	"excel_core_service/internal/records"
)

const (
	duplicateSourceTableID = "01J00000000000000000003001"
	duplicateTargetTableID = "01J00000000000000000003002"
	duplicateUserID        = "01J00000000000000000003003"
	duplicateCreatorID     = "01J00000000000000000003004"
	duplicateTextFieldID   = "01J00000000000000000003005"
	duplicateRelationID    = "01J00000000000000000003006"
	duplicateFormulaID     = "01J00000000000000000003007"
	duplicateRecordAID     = "01J00000000000000000003008"
	duplicateRecordBID     = "01J00000000000000000003009"
	duplicateEnterpriseID  = "01J00000000000000000000901"
	duplicateBaseID        = "01J00000000000000000000902"

	duplicateTargetTextFieldID = "01J00000000000000000003101"
	duplicateTargetRelationID  = "01J00000000000000000003102"
	duplicateTargetFormulaID   = "01J00000000000000000003103"
)

func incomingPrincipalContext(userID string) context.Context {
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: duplicateEnterpriseID,
		Internal:     true,
	})
	if err != nil {
		panic(err)
	}
	return metadata.NewIncomingContext(context.Background(), metadata.MD{
		"x-principal": []string{base64.StdEncoding.EncodeToString(payload)},
	})
}

// TestTableRowsDuplicatorCopiesSingleTableWithSelfRelation locks the reusable
// single-table seam: 行值保留、版本重置、created_by/at 按新记录语义、自关系值
// 改写到副本记录、公式物化值随行迁移、历史与评论不跟随。
func TestTableRowsDuplicatorCopiesSingleTableWithSelfRelation(t *testing.T) {
	db := openDuplicateFixtureDB(t)
	t.Cleanup(func() { deleteDuplicateFixtureRows(t, db) })
	seedDuplicateFixtureStructure(t, db)
	seedDuplicateFixtureRows(t, db)

	duplicator := records.NewTableRowsDuplicator(
		base.NewRecordsRepository(db),
		base.NewStructureRepository(db),
		base.NewCalcRepository(db),
		nil,
	)
	ctx := incomingPrincipalContext(duplicateUserID)
	if err := duplicator.DuplicateTableRows(ctx, duplicateSourceTableID, duplicateTargetTableID); err != nil {
		t.Fatalf("DuplicateTableRows() error = %v", err)
	}

	repository := base.NewRecordsRepository(db)
	copied, err := repository.RecordsByTable(context.Background(), duplicateTargetTableID, "")
	if err != nil {
		t.Fatalf("RecordsByTable(copy) error = %v", err)
	}
	if len(copied) != 2 {
		t.Fatalf("copied records = %d, want 2", len(copied))
	}
	byTitle := make(map[string]base.Record, len(copied))
	for _, record := range copied {
		byTitle[record.Cells[duplicateTargetTextFieldID].GetStringValue()] = record
	}
	copyA, ok := byTitle["任务A"]
	if !ok {
		t.Fatalf("copied records miss 任务A: %+v", copied)
	}
	copyB, ok := byTitle["任务B"]
	if !ok {
		t.Fatalf("copied records miss 任务B: %+v", copied)
	}
	if got := copyA.Cells[duplicateTargetRelationID].GetStringValue(); got != copyB.ID {
		t.Fatalf("copied self-relation cell = %q, want copied record %q", got, copyB.ID)
	}
	if copyA.Version != 1 || copyB.Version != 1 {
		t.Fatalf("copied versions = %d/%d, want 1/1 (new-record semantics)", copyA.Version, copyB.Version)
	}
	if copyA.CreatedBy != duplicateUserID || copyB.CreatedBy != duplicateUserID {
		t.Fatalf("copied created_by = %q/%q, want operator %q", copyA.CreatedBy, copyB.CreatedBy, duplicateUserID)
	}
	calcRepository := base.NewCalcRepository(db)
	value, ok, err := calcRepository.Value(context.Background(), copyA.ID, duplicateTargetFormulaID)
	if err != nil || !ok {
		t.Fatalf("copied formula materialization ok = %v err = %v, want carried value", ok, err)
	}
	if !proto.Equal(value, &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: 7}}) {
		t.Fatalf("copied formula value = %v, want 7", value)
	}
	if got := countRows(t, db, "SELECT COUNT(*) FROM excel_records_histories WHERE record_id = ?", copyA.ID); got != 0 {
		t.Fatalf("copied record histories = %d, want 0", got)
	}
}

func openDuplicateFixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testmysql.OpenTestDB(t, recordsTestMySQLDSN)
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	deleteDuplicateFixtureRows(t, db)
	return db
}

// seedDuplicateFixtureStructure 落一对源/副本表结构：字段名一致、副本关系字段
// 指向副本表（与 structure.DuplicateTable 的结构复制产物一致）。
func seedDuplicateFixtureStructure(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	structureRepository := base.NewStructureRepository(db)
	textConfig, err := base.EncodeStructureConfig(map[string]any{})
	if err != nil {
		t.Fatalf("encode text config: %v", err)
	}
	sourceRelationConfig, err := base.EncodeStructureConfig(map[string]any{
		"target_table_id": duplicateSourceTableID,
		"allow_multiple":  false,
	})
	if err != nil {
		t.Fatalf("encode source relation config: %v", err)
	}
	targetRelationConfig, err := base.EncodeStructureConfig(map[string]any{
		"target_table_id": duplicateTargetTableID,
		"allow_multiple":  false,
	})
	if err != nil {
		t.Fatalf("encode target relation config: %v", err)
	}
	formulaConfig, err := base.EncodeStructureConfig(map[string]any{"expression": "1"})
	if err != nil {
		t.Fatalf("encode formula config: %v", err)
	}
	for _, table := range []base.StructureTable{
		{ID: duplicateSourceTableID, EnterpriseID: duplicateEnterpriseID, BaseID: duplicateBaseID, Name: "任务", CreatedAt: now},
		{ID: duplicateTargetTableID, EnterpriseID: duplicateEnterpriseID, BaseID: duplicateBaseID, Name: "任务 的副本", CreatedAt: now},
	} {
		if err := structureRepository.CreateTable(context.Background(), table); err != nil {
			t.Fatalf("seed table %s: %v", table.ID, err)
		}
	}
	fields := []base.StructureField{
		{ID: duplicateTextFieldID, TableID: duplicateSourceTableID, Name: "标题", Type: "FIELD_TYPE_TEXT", Config: textConfig, IsPrimary: true, CreatedAt: now},
		{ID: duplicateRelationID, TableID: duplicateSourceTableID, Name: "上级任务", Type: "FIELD_TYPE_RELATION", Config: sourceRelationConfig, CreatedAt: now},
		{ID: duplicateFormulaID, TableID: duplicateSourceTableID, Name: "合计", Type: "FIELD_TYPE_FORMULA", Config: formulaConfig, CreatedAt: now},
	}
	for index, field := range fields {
		field.Position = index
		if err := structureRepository.CreateField(context.Background(), field, nil); err != nil {
			t.Fatalf("seed source field %s: %v", field.ID, err)
		}
	}
	targetFields := []base.StructureField{
		{ID: duplicateTargetTextFieldID, TableID: duplicateTargetTableID, Name: "标题", Type: "FIELD_TYPE_TEXT", Config: textConfig, IsPrimary: true, CreatedAt: now},
		{ID: duplicateTargetRelationID, TableID: duplicateTargetTableID, Name: "上级任务", Type: "FIELD_TYPE_RELATION", Config: targetRelationConfig, CreatedAt: now},
		{ID: duplicateTargetFormulaID, TableID: duplicateTargetTableID, Name: "合计", Type: "FIELD_TYPE_FORMULA", Config: formulaConfig, CreatedAt: now},
	}
	for index, field := range targetFields {
		field.Position = index
		if err := structureRepository.CreateField(context.Background(), field, nil); err != nil {
			t.Fatalf("seed target field %s: %v", field.ID, err)
		}
	}
}

func seedDuplicateFixtureRows(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UnixMilli()
	repository := base.NewRecordsRepository(db)
	tx, err := repository.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("records BeginTx() error = %v", err)
	}
	for _, record := range []base.Record{
		{
			ID: duplicateRecordAID, TableID: duplicateSourceTableID,
			Cells: map[string]*commonv1.Value{
				duplicateTextFieldID: {Kind: &commonv1.Value_StringValue{StringValue: "任务A"}},
				duplicateRelationID:  {Kind: &commonv1.Value_StringValue{StringValue: duplicateRecordBID}},
			},
			CreatedBy: duplicateCreatorID, CreatedAt: now, Version: 6,
		},
		{
			ID: duplicateRecordBID, TableID: duplicateSourceTableID,
			Cells: map[string]*commonv1.Value{
				duplicateTextFieldID: {Kind: &commonv1.Value_StringValue{StringValue: "任务B"}},
			},
			CreatedBy: duplicateCreatorID, CreatedAt: now, Version: 3,
		},
	} {
		if err := repository.CreateRecordTx(context.Background(), tx, record); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed record %s: %v", record.ID, err)
		}
	}
	if err := repository.AppendHistoryTx(context.Background(), tx, base.RecordHistory{
		ID:        "01J00000000000000000003104",
		RecordID:  duplicateRecordAID,
		FieldID:   duplicateTextFieldID,
		After:     &commonv1.Value{Kind: &commonv1.Value_StringValue{StringValue: "任务A"}},
		Operator:  duplicateCreatorID,
		CreatedAt: now,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed history: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seeded records: %v", err)
	}
	calcRepository := base.NewCalcRepository(db)
	calcTx, err := calcRepository.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("calc BeginTx() error = %v", err)
	}
	if err := calcRepository.SaveMaterialized(context.Background(), calcTx, base.CalcMaterializedRecord{
		RecordID:  duplicateRecordAID,
		FieldID:   duplicateFormulaID,
		ValueJSON: mustDuplicateValueJSON(t, &commonv1.Value{Kind: &commonv1.Value_IntValue{IntValue: 7}}),
		Status:    "IDLE",
	}); err != nil {
		_ = calcTx.Rollback()
		t.Fatalf("seed materialization: %v", err)
	}
	if err := calcTx.Commit(); err != nil {
		t.Fatalf("commit seeded materialization: %v", err)
	}
}

func mustDuplicateValueJSON(t *testing.T, value *commonv1.Value) []byte {
	t.Helper()
	payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(value)
	if err != nil {
		t.Fatalf("encode formula value: %v", err)
	}
	return payload
}

func deleteDuplicateFixtureRows(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{query: `DELETE FROM excel_calc_materialized WHERE record_id IN (?, ?) OR record_id IN (SELECT id FROM excel_records WHERE table_id IN (?, ?))`,
			args: []any{duplicateRecordAID, duplicateRecordBID, duplicateSourceTableID, duplicateTargetTableID}},
		{query: `DELETE FROM excel_records_histories WHERE record_id IN (?, ?) OR record_id IN (SELECT id FROM excel_records WHERE table_id IN (?, ?))`,
			args: []any{duplicateRecordAID, duplicateRecordBID, duplicateSourceTableID, duplicateTargetTableID}},
		{query: `DELETE FROM excel_records WHERE table_id IN (?, ?)`,
			args: []any{duplicateSourceTableID, duplicateTargetTableID}},
		{query: `DELETE FROM excel_structure_field_options WHERE field_id LIKE '01J00000000000000000003%'`},
		{query: `DELETE FROM excel_structure_fields WHERE table_id IN (?, ?)`,
			args: []any{duplicateSourceTableID, duplicateTargetTableID}},
		{query: `DELETE FROM excel_structure_views WHERE table_id IN (?, ?)`,
			args: []any{duplicateSourceTableID, duplicateTargetTableID}},
		{query: `DELETE FROM excel_structure_tables WHERE id IN (?, ?)`,
			args: []any{duplicateSourceTableID, duplicateTargetTableID}},
	} {
		if _, err := db.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatalf("delete duplicate fixture rows with %q: %v", statement.query, err)
		}
	}
}
