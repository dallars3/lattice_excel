package calc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"excel-libs/eventbus"
	calcapi "excel/api/calc/v1"
	commonv1 "excel/api/common/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"google.golang.org/protobuf/proto"
)

const (
	calcTestBaseID   = "01J00000000000000000011001"
	calcTestTableID  = "01J00000000000000000011002"
	calcTestRecordID = "01J00000000000000000011003"
	calcTestJobID    = "01J00000000000000000011004"
	calcTestSourceID = "01J00000000000000000011005"
	calcTestCalcID   = "01J00000000000000000011006"
	calcTestFinalID  = "01J00000000000000000011007"
)

type materializedWrite struct {
	recordID string
	fieldID  string
	value    *commonv1.Value
}

type recomputeMaterializer struct {
	mu         sync.Mutex
	fieldNames map[string]string
	row        map[string]*commonv1.Value
	writes     []materializedWrite
}

type crossTableMaterializer struct {
	mu     sync.Mutex
	writes []materializedWrite
}

func (materializer *crossTableMaterializer) WriteComputed(
	_ context.Context,
	recordID, fieldID string,
	value *commonv1.Value,
) error {
	materializer.mu.Lock()
	defer materializer.mu.Unlock()
	cloned := proto.Clone(value).(*commonv1.Value)
	materializer.writes = append(materializer.writes, materializedWrite{
		recordID: recordID,
		fieldID:  fieldID,
		value:    cloned,
	})
	return nil
}

func (materializer *crossTableMaterializer) ReadRowContext(
	_ context.Context,
	tableID, recordID string,
) (map[string]*commonv1.Value, error) {
	switch {
	case tableID == "table_b" && recordID == "record_b":
		return map[string]*commonv1.Value{
			"Source": {Kind: &commonv1.Value_IntValue{IntValue: 4}},
		}, nil
	case tableID == "table_a" && recordID == "record_a":
		return map[string]*commonv1.Value{
			"ALookup": {Kind: &commonv1.Value_IntValue{IntValue: 5}},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected row context %s/%s", tableID, recordID)
	}
}

func (materializer *crossTableMaterializer) RowsReferencing(
	_ context.Context,
	tableID, fieldID, targetRecordID string,
) ([]string, error) {
	if tableID == "table_a" && fieldID == "a_rel" && targetRecordID == "record_b" {
		return []string{"record_a"}, nil
	}
	return nil, fmt.Errorf(
		"unexpected relation lookup %s/%s -> %s",
		tableID,
		fieldID,
		targetRecordID,
	)
}

func newRecomputeMaterializer() *recomputeMaterializer {
	return &recomputeMaterializer{
		fieldNames: map[string]string{
			calcTestSourceID: "Source",
			calcTestCalcID:   "Calc",
			calcTestFinalID:  "Final",
		},
		row: map[string]*commonv1.Value{
			"Source": {
				Kind: &commonv1.Value_IntValue{IntValue: 2},
			},
		},
	}
}

func (materializer *recomputeMaterializer) WriteComputed(
	_ context.Context,
	recordID, fieldID string,
	value *commonv1.Value,
) error {
	materializer.mu.Lock()
	defer materializer.mu.Unlock()
	if recordID != calcTestRecordID {
		return fmt.Errorf("unexpected record_id %q", recordID)
	}
	name, ok := materializer.fieldNames[fieldID]
	if !ok {
		return fmt.Errorf("unexpected field_id %q", fieldID)
	}
	cloned := proto.Clone(value).(*commonv1.Value)
	materializer.writes = append(materializer.writes, materializedWrite{
		recordID: recordID,
		fieldID:  fieldID,
		value:    cloned,
	})
	materializer.row[name] = cloned
	return nil
}

func (materializer *recomputeMaterializer) ReadRowContext(
	_ context.Context,
	tableID, recordID string,
) (map[string]*commonv1.Value, error) {
	materializer.mu.Lock()
	defer materializer.mu.Unlock()
	if tableID != calcTestTableID || recordID != calcTestRecordID {
		return nil, fmt.Errorf("unexpected row %s/%s", tableID, recordID)
	}
	result := make(map[string]*commonv1.Value, len(materializer.row))
	for name, value := range materializer.row {
		result[name] = value
	}
	return result, nil
}

func (materializer *recomputeMaterializer) snapshotWrites() []materializedWrite {
	materializer.mu.Lock()
	defer materializer.mu.Unlock()
	return append([]materializedWrite(nil), materializer.writes...)
}

func TestCalcJobIdempotentConsumeMaterializesAndRebroadcastsChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db := openCalcTestDB(t)
	cleanupCalcFixture(db)
	t.Cleanup(func() { cleanupCalcFixture(db) })

	repository := base.NewCalcRepository(db)
	store := base.NewBusStore(db)
	outbox := bus.NewOutbox(store)
	broker := eventbus.NewMemoryBus()
	materializer := newRecomputeMaterializer()
	schema := staticSchemaProvider{fields: []FieldDefinition{
		{ID: calcTestSourceID, Name: "Source", TableID: calcTestTableID, Type: "FIELD_TYPE_NUMBER"},
		{
			ID: calcTestCalcID, Name: "Calc", TableID: calcTestTableID, Type: "FIELD_TYPE_FORMULA",
			Formula: "{Source}*2", FormulaRefs: []string{"Source"},
		},
		{
			ID: calcTestFinalID, Name: "Final", TableID: calcTestTableID, Type: "FIELD_TYPE_FORMULA",
			Formula: "{Calc}+1", FormulaRefs: []string{"Calc"},
		},
	}}
	fixedNow := time.Date(2026, time.November, 2, 10, 11, 12, 0, shanghaiLocation())
	service := NewService(repository, schema, materializer, outbox, func() time.Time { return fixedNow })

	consumer := bus.NewConsumer(store, broker, bus.ConsumerConfig{
		Retry: eventbus.RetryPolicy{
			Initial:     10 * time.Millisecond,
			Max:         20 * time.Millisecond,
			MaxAttempts: 3,
		},
	})
	if err := Consumers(consumer, service); err != nil {
		t.Fatalf("Consumers() error = %v", err)
	}
	changeSubscription, err := broker.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: "record.changed",
		Mode:  eventbus.DeliveryModePubSub,
	})
	if err != nil {
		t.Fatalf("subscribe record.changed error = %v", err)
	}
	defer changeSubscription.Close()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("consumer.Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopContext, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = consumer.Stop(stopContext)
	})

	firstEventID := "calc-test-job-event-1"
	secondEventID := "calc-test-job-event-2"
	enqueueCalcJobs(t, ctx, store, outbox, map[string]string{
		firstEventID:  calcTestJobID,
		secondEventID: calcTestJobID,
	}, fixedNow)
	forwarder := bus.NewForwarder(store, broker, bus.ForwarderConfig{BatchSize: 100})
	if _, err := forwarder.ForwardOnce(ctx); err != nil {
		t.Fatalf("ForwardOnce() jobs error = %v", err)
	}
	waitForOutboxStatus(t, ctx, db, []string{firstEventID, secondEventID}, "acked")

	if _, err := forwarder.ForwardOnce(ctx); err != nil {
		t.Fatalf("ForwardOnce() record.changed error = %v", err)
	}
	changes := receiveCalcChanges(t, ctx, changeSubscription, 2)

	writes := materializer.snapshotWrites()
	if len(writes) != 2 {
		t.Fatalf("materialized writes = %d (%+v), want 2", len(writes), writes)
	}
	if writes[0].fieldID != calcTestCalcID || writes[1].fieldID != calcTestFinalID {
		t.Fatalf("write order = %s, %s; want Calc, Final", writes[0].fieldID, writes[1].fieldID)
	}
	if got := writes[0].value.GetIntValue(); got != 4 {
		t.Fatalf("Calc value = %d, want 4", got)
	}
	if got := writes[1].value.GetIntValue(); got != 5 {
		t.Fatalf("Final value = %d, want 5", got)
	}

	if len(changes) != 2 {
		t.Fatalf("record.changed count = %d, want 2", len(changes))
	}
	for _, change := range changes {
		header := change.Header
		if header.Type != "record.changed" || header.Version != 1 ||
			header.BaseID != calcTestBaseID || header.Seq != 0 ||
			header.Producer != "calc" || len(header.EventID) != 26 {
			t.Fatalf("record.changed header = %+v", header)
		}
		var payload struct {
			RecordID   string `json:"record_id"`
			FieldID    string `json:"field_id"`
			ChangeType string `json:"change_type"`
		}
		if err := json.Unmarshal(change.Payload, &payload); err != nil {
			t.Fatalf("decode record.changed payload: %v", err)
		}
		if payload.RecordID != calcTestRecordID || payload.ChangeType != "calc" {
			t.Fatalf("record.changed payload = %+v", payload)
		}
	}

	jobStatus, err := repository.JobStatus(ctx, calcTestJobID)
	if err != nil {
		t.Fatalf("JobStatus() error = %v", err)
	}
	if jobStatus != "SUCCESS" {
		t.Fatalf("job status = %q, want SUCCESS", jobStatus)
	}
	statusResponse, err := service.GetRecalcStatus(ctx, &calcapi.GetRecalcStatusRequest{
		RecordId: calcTestRecordID,
		FieldId:  calcTestCalcID,
	})
	if err != nil {
		t.Fatalf("GetRecalcStatus() error = %v", err)
	}
	if got := statusResponse.GetStatus(); got != calcapi.RecalcStatus_RECALC_STATUS_IDLE {
		t.Fatalf("recalc status = %s, want RECALC_STATUS_IDLE", got)
	}
	assertPersistedRecalcStatus(t, ctx, repository, calcapi.RecalcStatus_RECALC_STATUS_CALC, "CALC", "")
	assertPersistedRecalcStatus(t, ctx, repository, calcapi.RecalcStatus_RECALC_STATUS_ERROR, "ERROR", "")
	assertPersistedRecalcStatus(t, ctx, repository, calcapi.RecalcStatus_RECALC_STATUS_ERROR, "ERROR", "除数为空")
}

func TestRecomputeTravelsRelationLookupAcrossTables(t *testing.T) {
	fields := []FieldDefinition{
		{
			ID: "a_rel", Name: "ARel", TableID: "table_a", Type: "FIELD_TYPE_RELATION",
			Relation: &RelationTarget{TableID: "table_b", FieldID: "b_formula"},
		},
		{
			ID: "a_lookup", Name: "ALookup", TableID: "table_a", Type: "FIELD_TYPE_LOOKUP",
			Lookup: &LookupSource{RelationFieldID: "a_rel", TargetFieldID: "b_formula"},
		},
		{
			ID: "a_formula", Name: "AFormula", TableID: "table_a", Type: "FIELD_TYPE_FORMULA",
			Formula: "{ALookup}+1", FormulaRefs: []string{"ALookup"},
		},
		{ID: "b_source", Name: "Source", TableID: "table_b", Type: "FIELD_TYPE_NUMBER"},
		{
			ID: "b_formula", Name: "BFormula", TableID: "table_b", Type: "FIELD_TYPE_FORMULA",
			Formula: "{Source}*2", FormulaRefs: []string{"Source"},
		},
	}
	materializer := &crossTableMaterializer{}
	service := NewService(
		nil,
		staticSchemaProvider{fields: fields},
		materializer,
		nil,
		time.Now,
	)
	cells, err := service.computeCells(context.Background(), calcJob{
		JobID:           "01J00000000000000000011999",
		TableID:         "table_b",
		FieldIDList:     []string{"b_formula"},
		RecordIDList:    []string{"record_b"},
		TriggerRecordID: "record_b",
		TriggerFieldID:  "b_source",
	})
	if err != nil {
		t.Fatalf("computeCells() error = %v", err)
	}
	if len(cells) != 2 || len(materializer.writes) != 2 {
		t.Fatalf("writes = %+v, want B formula then A formula", materializer.writes)
	}
	if materializer.writes[0].recordID != "record_b" ||
		materializer.writes[0].fieldID != "b_formula" ||
		materializer.writes[0].value.GetIntValue() != 8 {
		t.Fatalf("first write = %+v, want record_b/b_formula=8", materializer.writes[0])
	}
	if materializer.writes[1].recordID != "record_a" ||
		materializer.writes[1].fieldID != "a_formula" ||
		materializer.writes[1].value.GetIntValue() != 6 {
		t.Fatalf("second write = %+v, want record_a/a_formula=6", materializer.writes[1])
	}
}

func openCalcTestDB(t *testing.T) *sql.DB {
	t.Helper()
	config, err := base.LoadConfig(filepath.Join("..", "..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("base.LoadConfig() error = %v", err)
	}
	db, err := base.OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	return db
}

func cleanupCalcFixture(db *sql.DB) {
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, `
		DELETE FROM excel_bus_outbox
		WHERE base_id = ? OR event_id IN (?, ?)
	`, calcTestBaseID, "calc-test-job-event-1", "calc-test-job-event-2")
	_, _ = db.ExecContext(ctx, `DELETE FROM excel_calc_materialized WHERE record_id = ?`, calcTestRecordID)
	_, _ = db.ExecContext(ctx, `DELETE FROM excel_calc_jobs WHERE table_id = ?`, calcTestTableID)
}

func enqueueCalcJobs(
	t *testing.T,
	ctx context.Context,
	store *base.BusStore,
	outbox *bus.Outbox,
	jobIDs map[string]string,
	now time.Time,
) {
	t.Helper()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for eventID, jobID := range jobIDs {
		payload, err := json.Marshal(map[string]any{
			"job_id":        jobID,
			"table_id":      calcTestTableID,
			"field_id_list": []string{calcTestCalcID},
			"trigger": map[string]string{
				"record_id": calcTestRecordID,
				"field_id":  calcTestSourceID,
			},
		})
		if err != nil {
			t.Fatalf("marshal calc.job payload: %v", err)
		}
		event := eventbus.Event{
			Header: eventbus.EventHeader{
				EventID:    eventID,
				Type:       "calc.job",
				Version:    1,
				BaseID:     calcTestBaseID,
				OccurredAt: now.UnixMilli(),
				Producer:   "records",
			},
			Payload: payload,
		}
		if err := outbox.Enqueue(ctx, tx, event); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", eventID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit calc.job fixtures: %v", err)
	}
}

func waitForOutboxStatus(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	eventIDs []string,
	want string,
) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		for _, eventID := range eventIDs {
			var status string
			err := db.QueryRowContext(ctx, `
				SELECT status FROM excel_bus_outbox WHERE event_id = ?
			`, eventID).Scan(&status)
			if err != nil {
				t.Fatalf("query outbox status for %s: %v", eventID, err)
			}
			if status == want {
				count++
			}
		}
		if count == len(eventIDs) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for outbox status %q: %v", want, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("outbox events never reached status %q", want)
}

func receiveCalcChanges(
	t *testing.T,
	ctx context.Context,
	subscription eventbus.Subscription,
	count int,
) []eventbus.Event {
	t.Helper()
	changes := make([]eventbus.Event, 0, count)
	for len(changes) < count {
		receiveContext, cancel := context.WithTimeout(ctx, 8*time.Second)
		delivery, err := subscription.Receive(receiveContext)
		cancel()
		if err != nil {
			t.Fatalf("receive record.changed %d: %v", len(changes)+1, err)
		}
		if delivery.Event.Header.Type != "record.changed" {
			t.Fatalf("received type %q, want record.changed", delivery.Event.Header.Type)
		}
		changes = append(changes, delivery.Event)
		if err := subscription.Ack(ctx, delivery); err != nil {
			t.Fatalf("Ack() error = %v", err)
		}
	}
	return changes
}

func assertPersistedRecalcStatus(
	t *testing.T,
	ctx context.Context,
	repository *base.CalcRepository,
	want calcapi.RecalcStatus,
	stored string,
	reason string,
) {
	t.Helper()
	tx, err := repository.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() for status error = %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := repository.SaveMaterialized(ctx, tx, base.CalcMaterializedRecord{
		RecordID:  calcTestRecordID,
		FieldID:   calcTestCalcID,
		ValueJSON: []byte(`{}`),
		Status:    stored,
		Reason:    reason,
	}); err != nil {
		t.Fatalf("SaveMaterialized(%s) error = %v", stored, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit status %s: %v", stored, err)
	}
	service := NewService(repository, nil, nil, nil, time.Now)
	response, err := service.GetRecalcStatus(ctx, &calcapi.GetRecalcStatusRequest{
		RecordId: calcTestRecordID,
		FieldId:  calcTestCalcID,
	})
	if err != nil {
		t.Fatalf("GetRecalcStatus(%s) error = %v", stored, err)
	}
	if response.GetStatus() != want {
		t.Fatalf("GetRecalcStatus(%s) = %s, want %s", stored, response.GetStatus(), want)
	}
	if response.GetReason() != reason {
		t.Fatalf("GetRecalcStatus(%s) reason = %q, want %q", stored, response.GetReason(), reason)
	}
}

func TestErrorCellValueSentinelEncoding(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   string
	}{
		{name: "with reason", reason: "除数为空", want: "#ERROR:除数为空"},
		{name: "empty reason", reason: "", want: "#ERROR"},
		{name: "blank reason", reason: "   ", want: "#ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := errorCellValue(tc.reason)
			if got := value.GetStringValue(); got != tc.want {
				t.Fatalf("errorCellValue(%q) = %q, want %q", tc.reason, got, tc.want)
			}
		})
	}
}

func TestComputeCellsEncodesFormulaErrorAsSentinel(t *testing.T) {
	fields := []FieldDefinition{
		{ID: "b_source", Name: "Source", TableID: "table_b", Type: "FIELD_TYPE_NUMBER"},
		{
			ID: "b_formula", Name: "BFormula", TableID: "table_b", Type: "FIELD_TYPE_FORMULA",
			Formula: "{Source}/0", FormulaRefs: []string{"Source"},
		},
	}
	materializer := &crossTableMaterializer{}
	service := NewService(nil, staticSchemaProvider{fields: fields}, materializer, nil, time.Now)
	cells, err := service.computeCells(context.Background(), calcJob{
		JobID:           "01J00000000000000000011997",
		TableID:         "table_b",
		FieldIDList:     []string{"b_formula"},
		RecordIDList:    []string{"record_b"},
		TriggerRecordID: "record_b",
		TriggerFieldID:  "b_source",
	})
	if err != nil {
		t.Fatalf("computeCells() error = %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("cells = %d, want 1", len(cells))
	}
	cell := cells[0]
	if cell.status != "ERROR" {
		t.Fatalf("status = %q, want ERROR", cell.status)
	}
	if cell.reason != "除数为零" {
		t.Fatalf("reason = %q, want 除数为零", cell.reason)
	}
	if got := cell.value.GetStringValue(); got != "#ERROR:除数为零" {
		t.Fatalf("value = %q, want #ERROR:除数为零", got)
	}
	if len(materializer.writes) != 1 {
		t.Fatalf("materialized writes = %d, want 1", len(materializer.writes))
	}
	if got := materializer.writes[0].value.GetStringValue(); got != "#ERROR:除数为零" {
		t.Fatalf("materialized write value = %q, want #ERROR:除数为零", got)
	}
}
