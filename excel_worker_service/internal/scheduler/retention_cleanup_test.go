// scheduler/retention_cleanup_test.go：归属 scheduler 包的保留期与埋点清理测试。
package scheduler

import (
	"context"
	"testing"
	"time"

	"excel-libs/testmysql"
	"excel_worker_service/base"
)

type fakeTrackingCleaner struct {
	called bool
	cutoff int64
}

func (f *fakeTrackingCleaner) CleanupTracking(_ context.Context, cutoff int64) error {
	f.called = true
	f.cutoff = cutoff
	return nil
}

func TestRetentionJobAppliesTrackingCleanupCutoff(t *testing.T) {
	fake := &fakeTrackingCleaner{}
	job := RetentionJob{Tracking: fake}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC).UnixMilli()

	if err := job.Run(context.Background(), now); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	wantCutoff := now - base.TrackingRetention.Milliseconds()
	if !fake.called || fake.cutoff != wantCutoff {
		t.Fatalf("CleanupTracking cutoff = %d (called = %v), want %d", fake.cutoff, fake.called, wantCutoff)
	}
}

func TestTrackingRetentionCleanupDeletesExpiredRows(t *testing.T) {
	db := testmysql.OpenTestDB(t, schedulerTestDSN)
	t.Cleanup(func() { _ = db.Close() })

	// 2026-11-18 数据事故护栏（规则见 excel-libs/testmysql）：worker 测试的跨库
	// 保留期清理引用必须指向 core 测试库（excel_core_test），不得触碰共享 dev 库
	// excel_core。注入经 TrackingRetentionRepository.WithCoreSchemaName
	// （core_schema.go），不再改写包级可变量。
	coreSchema := testmysql.TestSchemaName("excel_core")
	trackingTable := coreSchema + ".excel_tracking_events"

	var exists int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = ? AND table_name = 'excel_tracking_events'
	`, coreSchema).Scan(&exists); err != nil || exists == 0 {
		t.Skip(coreSchema + ".excel_tracking_events 尚未迁移（先跑 core 迁移/测试）")
	}

	const prefix = "01J00000000000000000009"
	cleanup := func() {
		_, _ = db.ExecContext(context.Background(),
			"DELETE FROM "+trackingTable+" WHERE event_id LIKE '"+prefix+"%'")
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now().UTC().UnixMilli()
	cutoff := now - base.TrackingRetention.Milliseconds()
	insert := func(eventID string, receivedAtMs int64) {
		t.Helper()
		if _, err := db.ExecContext(context.Background(), `
			INSERT INTO `+trackingTable+` (
				event_id, user_id, user_type, event_name, received_at_ms
			) VALUES (?, ?, 'external', 'fe_retention_test', ?)
		`, eventID, prefix+"u1", receivedAtMs); err != nil {
			t.Fatalf("seed tracking row %s: %v", eventID, err)
		}
	}

	expiredOld := prefix + "o1"
	expiredEdge := prefix + "o2"
	recent := prefix + "r1"
	insert(expiredOld, cutoff-1000)
	insert(expiredEdge, cutoff-1)
	insert(recent, now)

	repository := base.NewTrackingRetentionRepository(db).WithCoreSchemaName(coreSchema)
	if err := repository.CleanupTracking(context.Background(), cutoff); err != nil {
		t.Fatalf("CleanupTracking() error = %v", err)
	}

	rows, err := db.QueryContext(context.Background(), `
		SELECT event_id FROM `+trackingTable+`
		WHERE event_id LIKE '`+prefix+`%'
	`)
	if err != nil {
		t.Fatalf("query remaining tracking rows: %v", err)
	}
	defer rows.Close()
	remaining := map[string]bool{}
	for rows.Next() {
		var eventID string
		if err := rows.Scan(&eventID); err != nil {
			t.Fatalf("scan remaining tracking row: %v", err)
		}
		remaining[eventID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate remaining tracking rows: %v", err)
	}

	if remaining[expiredOld] || remaining[expiredEdge] {
		t.Fatalf("expired rows still present: remaining = %v", remaining)
	}
	if !remaining[recent] {
		t.Fatalf("recent row missing: remaining = %v", remaining)
	}
}