package records_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	recordsv1 "excel/api/records/v1"
	structurev1 "excel/api/structure/v1"
)

func TestGetEventsSinceReplaysEffectiveSequenceWithoutWriting(t *testing.T) {
	access := &multiStructureAccessor{
		tables: map[string]*structurev1.Table{},
		fields: map[string][]*structurev1.Field{},
		views:  map[string]*structurev1.View{},
		config: map[string]*structurev1.ViewConfig{},
	}
	harness := newQueryHarness(
		t,
		access,
		queryScopeProvider{scope: "ROW_SCOPE_ALL"},
		queryFormulaProvider{},
	)
	seedOutboxEvent(t, harness.db, 100, "evt_legacy", recordsTestBaseID, 0, recordsTestTableID, "legacy")
	seedOutboxEvent(t, harness.db, 101, "evt_seq_5", recordsTestBaseID, 5, recordsTestTableID, "seq-5")
	seedOutboxEvent(t, harness.db, 102, "evt_other_table", recordsTestBaseID, 6, recordsTestTargetTableID, "other-table")
	seedOutboxEvent(t, harness.db, 103, "evt_seq_7", recordsTestBaseID, 7, recordsTestTableID, "seq-7")
	seedOutboxEvent(t, harness.db, 104, "evt_other_base", "01J00000000000000000003000", 8, recordsTestTableID, "other-base")

	beforeCount := outboxCount(t, harness.db)
	stream, err := harness.client.GetEventsSince(
		recordsAuthenticatedContext(t, recordsTestUserID, "ROW_SCOPE_ALL"),
		&recordsv1.GetEventsSinceRequest{
			BaseId:  recordsTestBaseID,
			TableId: recordsTestTableID,
			LastSeq: 4,
			Limit:   10,
		},
	)
	if err != nil {
		t.Fatalf("GetEventsSince() error = %v", err)
	}
	got := make([]string, 0)
	sequences := make([]int64, 0)
	for {
		response, err := stream.Recv()
		if err != nil {
			break
		}
		got = append(got, response.GetEvent().GetHeader().GetEventId())
		sequences = append(sequences, response.GetEvent().GetHeader().GetSeq())
	}
	if fmt.Sprint(got) != fmt.Sprint([]string{"evt_seq_5", "evt_seq_7", "evt_legacy"}) {
		t.Fatalf("event order = %v, want seq_5, seq_7, legacy", got)
	}
	if fmt.Sprint(sequences) != fmt.Sprint([]int64{5, 7, 100}) {
		t.Fatalf("event effective sequences = %v, want [5 7 100]", sequences)
	}
	if afterCount := outboxCount(t, harness.db); afterCount != beforeCount {
		t.Fatalf("outbox count = %d, want unchanged %d", afterCount, beforeCount)
	}
}

func seedOutboxEvent(
	t *testing.T,
	db *sql.DB,
	id int64,
	eventID, baseID string,
	seq int64,
	tableID, label string,
) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"table_id":%q,"record_id":"rec_%s","label":%q}`,
		tableID,
		label,
		label,
	)
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_bus_outbox (
			id, event_id, type, version, base_id, seq, payload, status,
			occurred_at, producer, publish_attempts, consume_attempts,
			next_publish_at, created_at, updated_at
		) VALUES (?, ?, 'record.changed', 1, ?, ?, ?, 'sent', ?, 'records', 0, 0, ?, ?, ?)
	`,
		id,
		eventID,
		baseID,
		seq,
		[]byte(payload),
		time.UnixMilli(1_790_000_000_000).UTC(),
		time.UnixMilli(1_790_000_000_000).UTC(),
		time.UnixMilli(1_790_000_000_000).UTC(),
		time.UnixMilli(1_790_000_000_000).UTC(),
	)
	if err != nil {
		t.Fatalf("seed outbox event %s: %v", eventID, err)
	}
}

func outboxCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM excel_bus_outbox",
	).Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return count
}
