package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"excel-libs/eventbus"
	"excel_worker_service/base"
	workerbus "excel_worker_service/bus"
	filelogic "excel_worker_service/internal/file"
	importlogic "excel_worker_service/internal/importexport"
	notifylogic "excel_worker_service/internal/notify"
)

const schedulerTestDSN = "excel:excel123@tcp(127.0.0.1:3306)/excel_worker?charset=utf8mb4&parseTime=true"

func TestTaskRepositoryClaimsLeasesAndRetriesToDead(t *testing.T) {
	db := openSchedulerTestDB(t)
	repository := base.NewTaskRepository(db)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if err := repository.Enqueue(context.Background(), base.SchedulerTask{
		ID:         "01J00000000000000000000001",
		Type:       "deactivation",
		RunAt:      now.UnixMilli(),
		MaxRetries: 3,
		Payload:    []byte(`{"user_id":"user-a"}`),
		CreatedAt:  now.UnixMilli(),
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	claimed, err := repository.ClaimDue(
		context.Background(),
		now,
		"pid-a",
		time.Minute,
		10,
	)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 ||
		claimed[0].Status != base.TaskStatusRunning ||
		claimed[0].OwnerPID != "pid-a" ||
		claimed[0].LeaseUntil != now.Add(time.Minute).UnixMilli() {
		t.Fatalf("ClaimDue() = %+v", claimed)
	}
	if blocked, err := repository.ClaimDue(
		context.Background(),
		now.Add(30*time.Second),
		"pid-b",
		time.Minute,
		10,
	); err != nil || len(blocked) != 0 {
		t.Fatalf("ClaimDue(leased) = %+v, %v; want no task", blocked, err)
	}
	reclaimed, err := repository.ClaimDue(
		context.Background(),
		now.Add(61*time.Second),
		"pid-b",
		time.Minute,
		10,
	)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].OwnerPID != "pid-b" {
		t.Fatalf("ClaimDue(expired lease) = %+v, %v", reclaimed, err)
	}

	failed, err := repository.MarkFailed(
		context.Background(),
		reclaimed[0],
		now.Add(61*time.Second),
		10*time.Second,
		time.Minute,
		sql.ErrConnDone,
	)
	if err != nil {
		t.Fatalf("MarkFailed(1) error = %v", err)
	}
	if failed.Status != base.TaskStatusFailed ||
		failed.RetryCount != 1 ||
		failed.RunAt != now.Add(71*time.Second).UnixMilli() {
		t.Fatalf("MarkFailed(1) = %+v", failed)
	}
	second, err := repository.ClaimDue(
		context.Background(),
		now.Add(71*time.Second),
		"pid-c",
		time.Minute,
		10,
	)
	if err != nil || len(second) != 1 {
		t.Fatalf("ClaimDue(retry 2) = %+v, %v", second, err)
	}
	failed, err = repository.MarkFailed(
		context.Background(),
		second[0],
		now.Add(71*time.Second),
		10*time.Second,
		time.Minute,
		sql.ErrConnDone,
	)
	if err != nil {
		t.Fatalf("MarkFailed(2) error = %v", err)
	}
	if failed.Status != base.TaskStatusFailed ||
		failed.RetryCount != 2 ||
		failed.RunAt != now.Add(91*time.Second).UnixMilli() {
		t.Fatalf("MarkFailed(2) = %+v", failed)
	}
	third, err := repository.ClaimDue(
		context.Background(),
		now.Add(91*time.Second),
		"pid-d",
		time.Minute,
		10,
	)
	if err != nil || len(third) != 1 {
		t.Fatalf("ClaimDue(retry 3) = %+v, %v", third, err)
	}
	dead, err := repository.MarkFailed(
		context.Background(),
		third[0],
		now.Add(91*time.Second),
		10*time.Second,
		time.Minute,
		sql.ErrConnDone,
	)
	if err != nil {
		t.Fatalf("MarkFailed(3) error = %v", err)
	}
	if dead.Status != base.TaskStatusDead ||
		dead.RetryCount != 3 ||
		!strings.Contains(dead.LastError, sql.ErrConnDone.Error()) {
		t.Fatalf("MarkFailed(3) = %+v", dead)
	}
}

func TestEnterpriseDissolvedConsumerCleansOncePerEvent(t *testing.T) {
	db := openSchedulerTestDB(t)
	if _, err := db.ExecContext(
		context.Background(),
		"DELETE FROM excel_bus_consumer_events",
	); err != nil {
		t.Fatalf("clear consumer events: %v", err)
	}
	repositories := &fakeLifecycleRepositories{done: make(chan struct{})}
	cleanup, err := NewEnterpriseCleanup(EnterpriseCleanupDependencies{
		Files:         repositories,
		Tasks:         repositories,
		Notifications: repositories,
	})
	if err != nil {
		t.Fatalf("NewEnterpriseCleanup() error = %v", err)
	}
	broker := eventbus.NewMemoryBus()
	consumer := workerbus.NewConsumer(db, broker, workerbus.ConsumerConfig{})
	if err := Consumers(consumer, cleanup); err != nil {
		t.Fatalf("Consumers() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("consumer.Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = consumer.Stop(stopCtx)
	})

	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "01J00000000000000000000E17",
			Type:       "enterprise.dissolved",
			Version:    1,
			OccurredAt: time.Now().UTC().UnixMilli(),
			Producer:   "iam",
		},
		Payload: []byte(`{
			"enterprise_id":"01J0000000000000000000ENT1",
			"dissolved_at":1793793600000,
			"base_id_list":["01J0000000000000000000BASE1"],
			"members_ids":["01J0000000000000000000USER1"]
		}`),
	}
	if err := broker.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	if err := broker.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish(duplicate) error = %v", err)
	}
	select {
	case <-repositories.done:
	case <-time.After(3 * time.Second):
		t.Fatal("enterprise cleanup did not run")
	}
	time.Sleep(200 * time.Millisecond)
	repositories.assertCalledOnce(t)
}

func TestEnterpriseCleanupRepositoriesApplyScopedDeletes(t *testing.T) {
	db := openSchedulerTestDB(t)
	ctx := context.Background()
	attachDir := t.TempDir()
	files := base.NewFileRepository(db)
	tasks := base.NewImportExportRepository(db)
	notifications := base.NewNotifyRepository(db)
	fileService := filelogic.NewService(files, nil, attachDir, time.Now)
	importService := importlogic.NewService(files, tasks, nil, nil, nil, nil, attachDir, time.Now)
	notifyService := notifylogic.NewService(notifications, nil, nil, time.Now)
	cleanup, err := NewEnterpriseCleanup(EnterpriseCleanupDependencies{
		Files:         fileService,
		Tasks:         importService,
		Notifications: notifyService,
	})
	if err != nil {
		t.Fatalf("NewEnterpriseCleanup() error = %v", err)
	}

	const (
		enterpriseID = "01J0000000000000000000ENT2"
		baseID       = "01J000000000000000000BASE2"
		otherBaseID  = "01J000000000000000000BASE3"
		memberID     = "01J000000000000000000USER2"
		otherUserID  = "01J000000000000000000USER3"
		exportFileID = "01J000000000000000000FILE2"
		reportFileID = "01J000000000000000000FILE3"
	)
	if err := notifications.Create(ctx, base.Notification{
		ID:         "01J000000000000000000NOTE2",
		UserID:     memberID,
		Type:       "test",
		PayloadMap: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create member notification: %v", err)
	}
	if err := notifications.Create(ctx, base.Notification{
		ID:         "01J000000000000000000NOTE3",
		UserID:     otherUserID,
		Type:       "test",
		PayloadMap: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create other notification: %v", err)
	}
	for _, task := range []base.ImportExportTask{
		{
			ID:           "01J000000000000000000TASK2",
			Kind:         "export",
			Status:       "done",
			FileID:       exportFileID,
			BaseID:       baseID,
			ReportFileID: reportFileID,
			Payload:      []byte(`{"file_name":"export.csv"}`),
		},
		{
			ID:      "01J000000000000000000TASK3",
			Kind:    "export",
			Status:  "done",
			BaseID:  otherBaseID,
			Payload: []byte(`{}`),
		},
	} {
		if _, err := tasks.Create(ctx, task); err != nil {
			t.Fatalf("create import/export task: %v", err)
		}
	}

	baseFileID := "01J000000000000000000FILE4"
	otherFileID := "01J000000000000000000FILE5"
	for _, meta := range []base.FileMeta{
		newCleanupFile(baseFileID, "attachment", baseID),
		newCleanupFile(exportFileID, "export", ""),
		newCleanupFile(reportFileID, "import_report", ""),
		newCleanupFile(otherFileID, "attachment", otherBaseID),
	} {
		fullPath := filepath.Join(attachDir, meta.StoragePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create attachment dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(meta.ID), 0o600); err != nil {
			t.Fatalf("write attachment: %v", err)
		}
		if err := files.Create(ctx, meta); err != nil {
			t.Fatalf("create file metadata: %v", err)
		}
	}

	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "01J00000000000000000000E18",
			Type:       "enterprise.dissolved",
			Version:    1,
			OccurredAt: time.Now().UTC().UnixMilli(),
			Producer:   "iam",
		},
		Payload: []byte(`{
			"enterprise_id":"` + enterpriseID + `",
			"base_id_list":["` + baseID + `"],
			"members_ids":["` + memberID + `"]
		}`),
	}
	if err := cleanup.Consume(ctx, event); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if err := cleanup.Consume(ctx, event); err != nil {
		t.Fatalf("Consume(replay) error = %v", err)
	}

	for _, fileID := range []string{baseFileID, exportFileID, reportFileID} {
		if _, err := files.Get(ctx, fileID); !errors.Is(err, base.ErrFileNotFound) {
			t.Fatalf("Get(%s) error = %v, want not found", fileID, err)
		}
	}
	if _, err := files.Get(ctx, otherFileID); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
	cleanedTask, err := tasks.Get(ctx, "01J000000000000000000TASK2")
	if err != nil ||
		cleanedTask.Status != "cleaned" ||
		cleanedTask.FileID != "" ||
		cleanedTask.ReportFileID != "" {
		t.Fatalf("cleaned task = %+v, %v", cleanedTask, err)
	}
	otherTask, err := tasks.Get(ctx, "01J000000000000000000TASK3")
	if err != nil || otherTask.Status != "done" {
		t.Fatalf("unrelated task = %+v, %v", otherTask, err)
	}
	remaining, err := notifications.List(ctx, memberID, 0, nil, 10)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("member notifications = %+v, %v", remaining, err)
	}
	otherNotifications, err := notifications.List(ctx, otherUserID, 0, nil, 10)
	if err != nil || len(otherNotifications) != 1 {
		t.Fatalf("other notifications = %+v, %v", otherNotifications, err)
	}
}

func newCleanupFile(id, usage, baseID string) base.FileMeta {
	return base.FileMeta{
		ID:          id,
		Name:        id + ".bin",
		DisplayName: id + ".bin",
		Ext:         "bin",
		Mime:        "application/octet-stream",
		Size:        1,
		Usage:       usage,
		Uploader:    "01J000000000000000000USER9",
		UploadBase:  baseID,
		StoragePath: id + ".bin",
		Checksum:    strings.Repeat("a", 64),
		CreatedAt:   time.Now().UTC().UnixMilli(),
	}
}

func TestRunnerExecutesOnceAndStartsBackgroundLoop(t *testing.T) {
	db := openSchedulerTestDB(t)
	repository := base.NewTaskRepository(db)
	now := time.Date(2026, 11, 4, 13, 0, 0, 0, time.UTC)
	runner := NewRunner(repository, RunnerOptions{
		OwnerPID:      "test-runner",
		LeaseDuration: time.Minute,
		PollInterval:  10 * time.Millisecond,
		BatchSize:     10,
		RetryInitial:  time.Second,
		RetryMax:      time.Minute,
		Now:           func() time.Time { return now },
	})
	handled := make(chan string, 2)
	if err := runner.Register("deactivation", func(_ context.Context, payload []byte) error {
		handled <- string(payload)
		return nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := repository.Enqueue(context.Background(), base.SchedulerTask{
		ID:         "01J00000000000000000000002",
		Type:       "deactivation",
		RunAt:      now.UnixMilli(),
		MaxRetries: 3,
		Payload:    []byte(`{"user_id":"user-b"}`),
	}); err != nil {
		t.Fatalf("Enqueue(first) error = %v", err)
	}
	count, err := runner.RunOnce(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("RunOnce() = %d, %v; want 1", count, err)
	}
	if got := <-handled; decodedUserID(got) != "user-b" {
		t.Fatalf("handled payload = %q", got)
	}
	first, err := repository.Get(context.Background(), "01J00000000000000000000002")
	if err != nil || first.Status != base.TaskStatusSuccess {
		t.Fatalf("first task = %+v, %v", first, err)
	}

	if err := repository.Enqueue(context.Background(), base.SchedulerTask{
		ID:         "01J00000000000000000000003",
		Type:       "deactivation",
		RunAt:      now.UnixMilli(),
		MaxRetries: 3,
		Payload:    []byte(`{"user_id":"user-c"}`),
	}); err != nil {
		t.Fatalf("Enqueue(second) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case got := <-handled:
		if decodedUserID(got) != "user-c" {
			t.Fatalf("background handled payload = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background runner did not execute due task")
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := runner.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	second, err := repository.Get(context.Background(), "01J00000000000000000000003")
	if err != nil || second.Status != base.TaskStatusSuccess {
		t.Fatalf("second task = %+v, %v", second, err)
	}
}

func decodedUserID(payload string) string {
	var decoded struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal([]byte(payload), &decoded)
	return decoded.UserID
}

type fakeLifecycleRepositories struct {
	mu               sync.Mutex
	fileCleanups     int
	fileLookups      int
	taskMarks        int
	notificationRuns int
	done             chan struct{}
	invalid          string
}

func (repositories *fakeLifecycleRepositories) CleanupFiles(
	_ context.Context,
	baseIDs, generatedFileIDs []string,
) error {
	repositories.mu.Lock()
	defer repositories.mu.Unlock()
	repositories.fileCleanups++
	if len(baseIDs) != 1 || baseIDs[0] != "01J0000000000000000000BASE1" {
		repositories.invalid = "unexpected base ids"
	}
	if len(generatedFileIDs) != 1 || generatedFileIDs[0] != "01J0000000000000000000FILE" {
		repositories.invalid = "unexpected generated file ids"
	}
	return nil
}

func (repositories *fakeLifecycleRepositories) EnterpriseFileIDs(
	context.Context,
	[]string,
) ([]string, error) {
	repositories.mu.Lock()
	defer repositories.mu.Unlock()
	repositories.fileLookups++
	return []string{"01J0000000000000000000FILE"}, nil
}

func (repositories *fakeLifecycleRepositories) MarkEnterpriseCleaned(
	context.Context,
	[]string,
) error {
	repositories.mu.Lock()
	defer repositories.mu.Unlock()
	repositories.taskMarks++
	return nil
}

func (repositories *fakeLifecycleRepositories) CleanupNotifications(
	_ context.Context,
	memberIDs []string,
) error {
	repositories.mu.Lock()
	repositories.notificationRuns++
	if len(memberIDs) != 1 || memberIDs[0] != "01J0000000000000000000USER1" {
		repositories.invalid = "unexpected member ids"
	}
	close(repositories.done)
	repositories.mu.Unlock()
	return nil
}

func (repositories *fakeLifecycleRepositories) assertCalledOnce(t *testing.T) {
	t.Helper()
	repositories.mu.Lock()
	defer repositories.mu.Unlock()
	if repositories.fileCleanups != 1 ||
		repositories.fileLookups != 1 ||
		repositories.taskMarks != 1 ||
		repositories.notificationRuns != 1 {
		t.Fatalf("cleanup calls = files:%d lookup:%d mark:%d notify:%d, want all 1",
			repositories.fileCleanups,
			repositories.fileLookups,
			repositories.taskMarks,
			repositories.notificationRuns,
		)
	}
	if repositories.invalid != "" {
		t.Fatal(repositories.invalid)
	}
}

func openSchedulerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := base.OpenDB(schedulerTestDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		"DROP TABLE IF EXISTS excel_scheduler_tasks",
		"DROP TABLE IF EXISTS schema_migrations",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset scheduler migration state: %v", err)
		}
	}
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	for _, statement := range []string{
		"DELETE FROM excel_scheduler_tasks",
		"DELETE FROM excel_bus_consumer_events",
		"DELETE FROM excel_importexport_tasks",
		"DELETE FROM excel_file_meta",
		"DELETE FROM excel_notify_notifications",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("clear scheduler test data: %v", err)
		}
	}
	return db
}

func TestLifecycleIntakeMaterializesOneTaskPerEntity(t *testing.T) {
	db := openSchedulerTestDB(t)
	repository := base.NewTaskRepository(db)
	intake, err := NewLifecycleIntake(repository)
	if err != nil {
		t.Fatalf("NewLifecycleIntake() error = %v", err)
	}
	deadline := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC).UnixMilli()

	newDeactivation := func(eventID, entityID string) eventbus.Event {
		payload, err := json.Marshal(struct {
			EntityID   string `json:"entity_id"`
			Kind       string `json:"kind"`
			DeadlineMs int64  `json:"deadline_ms"`
		}{EntityID: entityID, Kind: "user", DeadlineMs: deadline})
		if err != nil {
			t.Fatalf("marshal deactivation payload: %v", err)
		}
		return eventbus.Event{
			Header: eventbus.EventHeader{
				EventID:    eventID,
				Type:       "deactivation.initiated",
				Version:    1,
				OccurredAt: time.Now().UTC().UnixMilli(),
				Producer:   "iam",
			},
			Payload: payload,
		}
	}

	if err := intake.Consume(context.Background(), newDeactivation("01J00000000000000000000AA1", "user-a")); err != nil {
		t.Fatalf("Consume(first) error = %v", err)
	}
	if err := intake.Consume(context.Background(), newDeactivation("01J00000000000000000000AA1", "user-a")); err != nil {
		t.Fatalf("Consume(same event id) error = %v", err)
	}
	if err := intake.Consume(context.Background(), newDeactivation("01J00000000000000000000AA2", "user-a")); err != nil {
		t.Fatalf("Consume(different event, same entity) error = %v", err)
	}
	if got := countLifecycleTasks(t, db, "deactivation", "user", "user-a"); got != 1 {
		t.Fatalf("lifecycle task count = %d, want 1", got)
	}
	typ, kind, entityID, eventID, runAt := lifecycleTaskRow(t, db, "01J00000000000000000000AA1")
	if typ != "deactivation" || kind != "user" || entityID != "user-a" ||
		eventID != "01J00000000000000000000AA1" || runAt != deadline {
		t.Fatalf("lifecycle task = type:%s kind:%s entity:%s event:%s run_at:%d", typ, kind, entityID, eventID, runAt)
	}

	if err := intake.Consume(context.Background(), newDeactivation("01J00000000000000000000AA3", "user-b")); err != nil {
		t.Fatalf("Consume(second entity) error = %v", err)
	}
	if got := countLifecycleTasks(t, db, "deactivation", "user", "user-b"); got != 1 {
		t.Fatalf("second entity task count = %d, want 1", got)
	}
}

func TestLifecycleIntakeMapsDissolutionToEnterpriseTask(t *testing.T) {
	db := openSchedulerTestDB(t)
	intake, err := NewLifecycleIntake(base.NewTaskRepository(db))
	if err != nil {
		t.Fatalf("NewLifecycleIntake() error = %v", err)
	}
	deadline := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC).UnixMilli()
	payload, err := json.Marshal(struct {
		EntityID   string `json:"entity_id"`
		Kind       string `json:"kind"`
		DeadlineMs int64  `json:"deadline_ms"`
	}{EntityID: "enterprise-1", Kind: "enterprise", DeadlineMs: deadline})
	if err != nil {
		t.Fatalf("marshal dissolution payload: %v", err)
	}
	event := eventbus.Event{
		Header: eventbus.EventHeader{
			EventID:    "01J00000000000000000000CC1",
			Type:       "dissolution.initiated",
			Version:    1,
			OccurredAt: time.Now().UTC().UnixMilli(),
			Producer:   "iam",
		},
		Payload: payload,
	}
	if err := intake.Consume(context.Background(), event); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	typ, kind, entityID, _, runAt := lifecycleTaskRow(t, db, "01J00000000000000000000CC1")
	if typ != "enterprise_dissolution" || kind != "enterprise" || entityID != "enterprise-1" || runAt != deadline {
		t.Fatalf("dissolution task = type:%s kind:%s entity:%s run_at:%d", typ, kind, entityID, runAt)
	}
}

func TestLifecycleIntakeRejectsBadPayload(t *testing.T) {
	db := openSchedulerTestDB(t)
	intake, err := NewLifecycleIntake(base.NewTaskRepository(db))
	if err != nil {
		t.Fatalf("NewLifecycleIntake() error = %v", err)
	}
	cases := []eventbus.Event{
		{
			Header: eventbus.EventHeader{
				EventID: "01J00000000000000000000BB1", Type: "deactivation.initiated", Version: 1, Producer: "iam",
			},
			Payload: []byte(`{"kind":"user","deadline_ms":1}`), // missing entity_id
		},
		{
			Header: eventbus.EventHeader{
				EventID: "01J00000000000000000000BB2", Type: "dissolution.initiated", Version: 1, Producer: "iam",
			},
			Payload: []byte(`{"entity_id":"e1","kind":"user","deadline_ms":1}`), // wrong kind
		},
		{
			Header: eventbus.EventHeader{
				EventID: "01J00000000000000000000BB3", Type: "other.topic", Version: 1, Producer: "iam",
			},
			Payload: []byte(`{"entity_id":"e1","kind":"user","deadline_ms":1}`), // unsupported topic
		},
	}
	for index, event := range cases {
		if err := intake.Consume(context.Background(), event); err == nil {
			t.Fatalf("case %d Consume() error = nil, want error", index)
		}
	}
	if got := countSchedulerTasks(db, context.Background(), t); got != 0 {
		t.Fatalf("scheduler task count = %d, want 0", got)
	}
}

func lifecycleTaskRow(t *testing.T, db *sql.DB, id string) (typ, kind, entityID, eventID string, runAt int64) {
	t.Helper()
	if err := db.QueryRowContext(context.Background(), `
		SELECT type, COALESCE(kind, ''), COALESCE(entity_id, ''), COALESCE(event_id, ''), run_at
		FROM excel_scheduler_tasks WHERE id = ?`, id).Scan(&typ, &kind, &entityID, &eventID, &runAt); err != nil {
		t.Fatalf("read lifecycle task: %v", err)
	}
	return typ, kind, entityID, eventID, runAt
}

func countLifecycleTasks(t *testing.T, db *sql.DB, typ, kind, entityID string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM excel_scheduler_tasks
		WHERE type = ? AND COALESCE(kind, '') = ? AND COALESCE(entity_id, '') = ?`,
		typ, kind, entityID).Scan(&count); err != nil {
		t.Fatalf("count lifecycle tasks: %v", err)
	}
	return count
}

func countSchedulerTasks(db *sql.DB, ctx context.Context, t *testing.T) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM excel_scheduler_tasks`).Scan(&count); err != nil {
		t.Fatalf("count scheduler tasks: %v", err)
	}
	return count
}
