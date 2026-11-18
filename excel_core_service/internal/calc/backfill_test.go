// calc/backfill_test.go：无触发记录作业（结构域回填种子）的全表分页、批次链与
// 空表/幂等边界。N5 缺陷：公式字段创建后存量记录不物化。
package calc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"google.golang.org/protobuf/proto"
)

const (
	calcBackfillBaseID    = "01J00000000000000000012001"
	calcBackfillTableID   = "01J00000000000000000012002"
	calcBackfillSourceID  = "01J00000000000000000012003"
	calcBackfillFormulaID = "01J00000000000000000012004"
	// 回填夹具的假记录统一前缀（23 字符 + 3 位序号 = 26 字符 ULID 列宽），
	// 清理按前缀隔离，不触碰其他套件的数据。
	calcBackfillRecordPrefix = "01JBK000000000000000000"
)

// backfillPager 以受控页应答 RecordIDsAfter，并记录游标调用序列。
type backfillPager struct {
	mu      sync.Mutex
	records []string
	cursors []string
}

func (pager *backfillPager) RecordIDsAfter(
	_ context.Context,
	tableID, afterRecordID string,
	limit int,
) ([]string, error) {
	pager.mu.Lock()
	defer pager.mu.Unlock()
	if tableID != calcBackfillTableID {
		return nil, fmt.Errorf("unexpected table %s", tableID)
	}
	pager.cursors = append(pager.cursors, afterRecordID)
	start := 0
	if afterRecordID != "" {
		for index, recordID := range pager.records {
			if recordID == afterRecordID {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(pager.records) {
		end = len(pager.records)
	}
	return append([]string(nil), pager.records[start:end]...), nil
}

func (pager *backfillPager) snapshotCursors() []string {
	pager.mu.Lock()
	defer pager.mu.Unlock()
	return append([]string(nil), pager.cursors...)
}

// backfillMaterializer 记录物化写入、回读固定行上下文（Source=2）并按受控页
// 应答分页接缝——一个适配器同时满足 calc 的三个接缝，与生产装配形状一致。
type backfillMaterializer struct {
	*backfillPager
	mu     sync.Mutex
	writes []materializedWrite
}

func newBackfillMaterializer(records []string) *backfillMaterializer {
	return &backfillMaterializer{
		backfillPager: &backfillPager{records: records},
	}
}

func (materializer *backfillMaterializer) WriteComputed(
	_ context.Context,
	recordID, fieldID string,
	value *commonv1.Value,
) error {
	materializer.mu.Lock()
	defer materializer.mu.Unlock()
	materializer.writes = append(materializer.writes, materializedWrite{
		recordID: recordID,
		fieldID:  fieldID,
		value:    proto.Clone(value).(*commonv1.Value),
	})
	return nil
}

func (materializer *backfillMaterializer) ReadRowContext(
	_ context.Context,
	tableID, recordID string,
) (map[string]*commonv1.Value, error) {
	if tableID != calcBackfillTableID || recordID == "" {
		return nil, fmt.Errorf("unexpected row %s/%s", tableID, recordID)
	}
	return map[string]*commonv1.Value{
		"Source": {Kind: &commonv1.Value_IntValue{IntValue: 2}},
	}, nil
}

func (materializer *backfillMaterializer) snapshotWrites() []materializedWrite {
	materializer.mu.Lock()
	defer materializer.mu.Unlock()
	return append([]materializedWrite(nil), materializer.writes...)
}

func backfillRecordID(index int) string {
	return fmt.Sprintf("%s%03d", calcBackfillRecordPrefix, index)
}

func cleanupBackfillFixture(db *sql.DB) {
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, `DELETE FROM excel_bus_outbox WHERE base_id = ?`, calcBackfillBaseID)
	_, _ = db.ExecContext(ctx, `DELETE FROM excel_calc_jobs WHERE table_id = ?`, calcBackfillTableID)
	_, _ = db.ExecContext(ctx, `DELETE FROM excel_calc_materialized WHERE record_id LIKE ?`,
		calcBackfillRecordPrefix+"%")
}

func backfillSchema() staticSchemaProvider {
	return staticSchemaProvider{fields: []FieldDefinition{
		{ID: calcBackfillSourceID, Name: "Source", TableID: calcBackfillTableID, Type: "FIELD_TYPE_NUMBER"},
		{
			ID: calcBackfillFormulaID, Name: "Calc", TableID: calcBackfillTableID, Type: "FIELD_TYPE_FORMULA",
			Formula: "{Source}*2", FormulaRefs: []string{"Source"},
		},
	}}
}

func enqueueBackfillSeed(t *testing.T, ctx context.Context, store *base.BusStore, outbox *bus.Outbox, jobID string, now time.Time) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"job_id":        jobID,
		"table_id":      calcBackfillTableID,
		"field_id_list": []string{calcBackfillFormulaID},
		"trigger":       map[string]string{"record_id": "", "field_id": ""},
		"backfill":      map[string]any{},
	})
	if err != nil {
		t.Fatalf("marshal backfill seed payload: %v", err)
	}
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "backfill-seed-" + jobID,
			Type:       "calc.job",
			Version:    1,
			BaseID:     calcBackfillBaseID,
			OccurredAt: now.UnixMilli(),
			Producer:   "structure",
		},
		Payload: payload,
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := outbox.Enqueue(ctx, tx, event); err != nil {
		t.Fatalf("Enqueue(seed) error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed event: %v", err)
	}
	return event
}

// pendingCalcJobEvents 读取 base 下 producer=calc 的链式回填作业事件（按
// event_id 升序）。种子事件 producer=structure，不计入——测试直接调用消费
// 处理器，不经过 forwarder/consumer 的 ack 状态机。
func pendingCalcJobEvents(t *testing.T, ctx context.Context, db *sql.DB, baseID string) []eventbus.Event {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT event_id, payload
		FROM excel_bus_outbox
		WHERE base_id = ? AND type = 'calc.job' AND producer = 'calc' AND status IN ('pending', 'sent')
		ORDER BY event_id
	`, baseID)
	if err != nil {
		t.Fatalf("query pending calc.job events: %v", err)
	}
	defer rows.Close()
	events := make([]eventbus.Event, 0)
	for rows.Next() {
		var eventID string
		var payload []byte
		if err := rows.Scan(&eventID, &payload); err != nil {
			t.Fatalf("scan pending calc.job event: %v", err)
		}
		events = append(events, eventbus.Event{
			Header: eventbus.EventHeader{
				EventID:  eventID,
				Type:     "calc.job",
				Version:  1,
				BaseID:   baseID,
				Producer: "structure",
			},
			Payload: payload,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pending calc.job events: %v", err)
	}
	return events
}

// ackOutboxEvent 模拟 consumer 的 ack：测试直接调用消费处理器，不经状态机，
// 事件行删除后 pending 查询才反映「已处理」。
func ackOutboxEvent(t *testing.T, ctx context.Context, db *sql.DB, eventID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM excel_bus_outbox WHERE event_id = ?`, eventID); err != nil {
		t.Fatalf("ack outbox event %s: %v", eventID, err)
	}
}

type backfillChainPayload struct {
	JobID       string   `json:"job_id"`
	TableID     string   `json:"table_id"`
	FieldIDList []string `json:"field_id_list"`
	Trigger     struct {
		RecordID string `json:"record_id"`
		FieldID  string `json:"field_id"`
	} `json:"trigger"`
	Backfill *struct {
		AfterRecordID string `json:"after_record_id"`
	} `json:"backfill"`
}

// TestBackfillJobPagesWholeTableAndChainsNextBatch 覆盖 N5 分批回填主链路：
// 501 条存量记录 → 种子作业物化前 500 条并同事务入队下一批（游标=第 500 条）；
// 链式作业物化余下 1 条且不再入队；重复投递种子作业被作业去重键挡下。
func TestBackfillJobPagesWholeTableAndChainsNextBatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	db := openCalcTestDB(t)
	cleanupBackfillFixture(db)
	t.Cleanup(func() { cleanupBackfillFixture(db) })

	repository := base.NewCalcRepository(db)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	const total = backfillPageSize + 1
	records := make([]string, 0, total)
	for index := 0; index < total; index++ {
		records = append(records, backfillRecordID(index))
	}
	materializer := newBackfillMaterializer(records)
	fixedNow := time.Date(2026, time.November, 19, 10, 0, 0, 0, shanghaiLocation())
	service := NewService(repository, backfillSchema(), materializer, outbox, func() time.Time { return fixedNow })

	seedJobID := "01J00000000000000000012999"
	seedEvent := enqueueBackfillSeed(t, ctx, store, outbox, seedJobID, fixedNow)

	if err := service.consumeCalcJob(ctx, seedEvent); err != nil {
		t.Fatalf("consumeCalcJob(seed) error = %v", err)
	}
	status, err := repository.JobStatus(ctx, seedJobID)
	if err != nil {
		t.Fatalf("JobStatus(seed) error = %v", err)
	}
	if status != "SUCCESS" {
		t.Fatalf("seed job status = %q, want SUCCESS", status)
	}
	writes := materializer.snapshotWrites()
	if len(writes) != backfillPageSize {
		t.Fatalf("materialized writes after seed = %d, want %d", len(writes), backfillPageSize)
	}
	for _, write := range writes {
		if write.fieldID != calcBackfillFormulaID || write.value.GetIntValue() != 4 {
			t.Fatalf("unexpected write %+v", write)
		}
	}

	// 种子作业必须在同一事务内入队下一批（游标=本批最后一条记录）。
	chained := pendingCalcJobEvents(t, ctx, db, calcBackfillBaseID)
	if len(chained) != 1 {
		t.Fatalf("chained calc.job events = %d, want 1", len(chained))
	}
	var chainPayload backfillChainPayload
	if err := json.Unmarshal(chained[0].Payload, &chainPayload); err != nil {
		t.Fatalf("decode chained payload: %v", err)
	}
	if chainPayload.JobID == "" || chainPayload.JobID == seedJobID {
		t.Fatalf("chained job_id = %q, want a fresh ULID", chainPayload.JobID)
	}
	if chainPayload.TableID != calcBackfillTableID ||
		len(chainPayload.FieldIDList) != 1 || chainPayload.FieldIDList[0] != calcBackfillFormulaID {
		t.Fatalf("chained payload = %+v", &chainPayload)
	}
	if chainPayload.Backfill == nil || chainPayload.Backfill.AfterRecordID != backfillRecordID(backfillPageSize-1) {
		t.Fatalf("chained backfill cursor = %+v, want %s", chainPayload.Backfill, backfillRecordID(backfillPageSize-1))
	}
	if chainPayload.Trigger.RecordID != "" {
		t.Fatalf("chained trigger record = %q, want empty (backfill carries no trigger)", chainPayload.Trigger.RecordID)
	}

	// 链式作业物化余下记录，且不再入队下一批。
	if err := service.consumeCalcJob(ctx, chained[0]); err != nil {
		t.Fatalf("consumeCalcJob(chained) error = %v", err)
	}
	ackOutboxEvent(t, ctx, db, chained[0].Header.EventID)
	status, err = repository.JobStatus(ctx, chainPayload.JobID)
	if err != nil {
		t.Fatalf("JobStatus(chained) error = %v", err)
	}
	if status != "SUCCESS" {
		t.Fatalf("chained job status = %q, want SUCCESS", status)
	}
	writes = materializer.snapshotWrites()
	if len(writes) != total {
		t.Fatalf("materialized writes after chain = %d, want %d", len(writes), total)
	}
	if writes[total-1].recordID != backfillRecordID(total-1) {
		t.Fatalf("last write = %s, want %s", writes[total-1].recordID, backfillRecordID(total-1))
	}
	if remaining := pendingCalcJobEvents(t, ctx, db, calcBackfillBaseID); len(remaining) != 0 {
		t.Fatalf("pending calc.job events after chain = %d, want 0", len(remaining))
	}
	if cursors := materializer.snapshotCursors(); len(cursors) != 2 || cursors[0] != "" ||
		cursors[1] != backfillRecordID(backfillPageSize-1) {
		t.Fatalf("pager cursors = %v, want [\"\", %s]", cursors, backfillRecordID(backfillPageSize-1))
	}

	// 幂等重跑：同一作业事件重复投递不产生重复副作用（CreateJob 去重）。
	if err := service.consumeCalcJob(ctx, seedEvent); err != nil {
		t.Fatalf("consumeCalcJob(seed replay) error = %v", err)
	}
	if replayWrites := materializer.snapshotWrites(); len(replayWrites) != total {
		t.Fatalf("materialized writes after replay = %d, want %d (no duplicate side effects)", len(replayWrites), total)
	}
	if replayPending := pendingCalcJobEvents(t, ctx, db, calcBackfillBaseID); len(replayPending) != 0 {
		t.Fatalf("pending calc.job events after replay = %d, want 0", len(replayPending))
	}
}

// TestBackfillJobOnEmptyTableSucceedsWithoutChain：空表 → 单空作业成功、零物化、
// 不入队下一批（分批边界：空表）。
func TestBackfillJobOnEmptyTableSucceedsWithoutChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := openCalcTestDB(t)
	cleanupBackfillFixture(db)
	t.Cleanup(func() { cleanupBackfillFixture(db) })

	repository := base.NewCalcRepository(db)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	materializer := newBackfillMaterializer(nil) // 无记录 → 空页
	fixedNow := time.Date(2026, time.November, 19, 10, 0, 0, 0, shanghaiLocation())
	service := NewService(repository, backfillSchema(), materializer, outbox, func() time.Time { return fixedNow })

	seedJobID := "01J00000000000000000012998"
	seedEvent := enqueueBackfillSeed(t, ctx, store, outbox, seedJobID, fixedNow)
	if err := service.consumeCalcJob(ctx, seedEvent); err != nil {
		t.Fatalf("consumeCalcJob(seed) error = %v", err)
	}
	status, err := repository.JobStatus(ctx, seedJobID)
	if err != nil {
		t.Fatalf("JobStatus() error = %v", err)
	}
	if status != "SUCCESS" {
		t.Fatalf("empty-table job status = %q, want SUCCESS", status)
	}
	if writes := materializer.snapshotWrites(); len(writes) != 0 {
		t.Fatalf("materialized writes = %d, want 0", len(writes))
	}
	if pending := pendingCalcJobEvents(t, ctx, db, calcBackfillBaseID); len(pending) != 0 {
		t.Fatalf("pending calc.job events = %d, want 0", len(pending))
	}
}

// TestBackfillJobSkipsCrossTableDependents：回填作业只物化本表受影响公式。
// 跨表依赖（其他表经 lookup→relation 链聚合本表字段）不展开跨表行集——其
// 重算由既有写链/按行 calc.job 触发路径负责。
func TestBackfillJobSkipsCrossTableDependents(t *testing.T) {
	const (
		otherTableID    = "01J00000000000000000012005"
		otherRelationID = "01J00000000000000000012006"
		otherLookupID   = "01J00000000000000000012007"
		otherFormulaID  = "01J00000000000000000012008"
	)
	materializer := &backfillMaterializer{}
	service := NewService(nil, staticSchemaProvider{fields: []FieldDefinition{
		{ID: calcBackfillSourceID, Name: "Source", TableID: calcBackfillTableID, Type: "FIELD_TYPE_NUMBER"},
		{
			ID: calcBackfillFormulaID, Name: "Calc", TableID: calcBackfillTableID, Type: "FIELD_TYPE_FORMULA",
			Formula: "{Source}*2", FormulaRefs: []string{"Source"},
		},
		{
			ID: otherRelationID, Name: "ORel", TableID: otherTableID, Type: "FIELD_TYPE_RELATION",
			Relation: &RelationTarget{TableID: calcBackfillTableID, FieldID: calcBackfillFormulaID},
		},
		{
			ID: otherLookupID, Name: "OLookup", TableID: otherTableID, Type: "FIELD_TYPE_LOOKUP",
			Lookup: &LookupSource{RelationFieldID: otherRelationID, TargetFieldID: calcBackfillFormulaID},
		},
		{
			ID: otherFormulaID, Name: "OFormula", TableID: otherTableID, Type: "FIELD_TYPE_FORMULA",
			Formula: "{OLookup}+1", FormulaRefs: []string{"OLookup"},
		},
	}}, materializer, nil, time.Now)
	cells, err := service.computeCells(context.Background(), calcJob{
		JobID:       "01J00000000000000000012997",
		BaseID:      calcBackfillBaseID,
		TableID:     calcBackfillTableID,
		FieldIDList: []string{calcBackfillFormulaID},
		RecordIDList: []string{
			backfillRecordID(0),
			backfillRecordID(1),
		},
		Backfill: true,
	})
	if err != nil {
		t.Fatalf("computeCells() error = %v", err)
	}
	if len(cells) != 2 {
		t.Fatalf("cells = %d, want 2 (same-table records only)", len(cells))
	}
	for _, cell := range cells {
		if cell.field.ID != calcBackfillFormulaID {
			t.Fatalf("cell field = %s, want %s", cell.field.ID, calcBackfillFormulaID)
		}
	}
	if writes := materializer.writes; len(writes) != 2 {
		t.Fatalf("materializer writes = %d, want 2 (no cross-table writes)", len(writes))
	}
}

// TestBackfillJobWithoutPagerSeamFailsFast：回填作业缺分页接缝时快速失败，
// 作业标记 ERROR 而不是静默空转。
func TestBackfillJobWithoutPagerSeamFailsFast(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := openCalcTestDB(t)
	cleanupBackfillFixture(db)
	t.Cleanup(func() { cleanupBackfillFixture(db) })

	repository := base.NewCalcRepository(db)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	materializer := &recomputeMaterializer{} // 无 RecordIDsAfter 接缝
	fixedNow := time.Date(2026, time.November, 19, 10, 0, 0, 0, shanghaiLocation())
	service := NewService(repository, backfillSchema(), materializer, outbox, func() time.Time { return fixedNow })

	seedJobID := "01J00000000000000000012996"
	seedEvent := enqueueBackfillSeed(t, ctx, store, outbox, seedJobID, fixedNow)
	if err := service.consumeCalcJob(ctx, seedEvent); err == nil {
		t.Fatal("consumeCalcJob(seed) error = nil, want pager seam error")
	}
	status, err := repository.JobStatus(ctx, seedJobID)
	if err != nil {
		t.Fatalf("JobStatus() error = %v", err)
	}
	if status != "ERROR" {
		t.Fatalf("job status = %q, want ERROR", status)
	}
}
