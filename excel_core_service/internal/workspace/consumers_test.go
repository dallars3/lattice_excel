// workspace/consumers_test.go：import.finished 消费者的解码与解锁行为（ticket 16）。
package workspace

import (
	"context"
	"encoding/json"
	"testing"

	"excel-libs/eventbus"

	"excel_core_service/base"
)

const consumersTestMySQLDSN = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"

func TestConsumeImportFinishedClearsImportLock(t *testing.T) {
	db, err := base.OpenDB(consumersTestMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	defer db.Close()
	if err := base.MigrateUp(db); err != nil {
		t.Fatalf("base.MigrateUp() error = %v", err)
	}

	const baseID = "01JIMPORTLOCKBASE000000000"
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_workspace_bases (
			id, enterprise_id, owner_id, name, last_modified_at,
			last_modifier, created_at, import_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'importing')
		ON DUPLICATE KEY UPDATE import_status = 'importing'
	`, baseID, "01JIMPORTLOCKENT000000000", "01JIMPORTLOCKUSR000000000",
		"导入中", 1762075200000, "tester", 1762075200000); err != nil {
		t.Fatalf("seed importing Base: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM excel_workspace_bases WHERE id = ?`, baseID)
	})

	service := &Service{repository: base.NewWorkspaceRepository(db)}
	payload, err := json.Marshal(map[string]any{"base_id": baseID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := service.consumeImportFinished(context.Background(), eventbus.Event{
		Header:  eventbus.EventHeader{EventID: "evt-1", Type: "import.finished"},
		Payload: payload,
	}); err != nil {
		t.Fatalf("consumeImportFinished() error = %v", err)
	}
	importing, err := base.NewWorkspaceRepository(db).Importing(context.Background(), baseID)
	if err != nil {
		t.Fatalf("Importing() error = %v", err)
	}
	if importing {
		t.Fatal("Importing() = true after import.finished, want false")
	}

	// 重复投递幂等；空 base_id 与坏 payload 的行为契约。
	if err := service.consumeImportFinished(context.Background(), eventbus.Event{
		Header:  eventbus.EventHeader{EventID: "evt-1", Type: "import.finished"},
		Payload: payload,
	}); err != nil {
		t.Fatalf("repeated consumeImportFinished() error = %v", err)
	}
	if err := service.consumeImportFinished(context.Background(), eventbus.Event{
		Header: eventbus.EventHeader{EventID: "evt-2", Type: "import.finished"},
	}); err != nil {
		t.Fatalf("consumeImportFinished(empty base_id) error = %v", err)
	}
	if err := service.consumeImportFinished(context.Background(), eventbus.Event{
		Header:  eventbus.EventHeader{EventID: "evt-3", Type: "import.finished"},
		Payload: []byte("{broken"),
	}); err == nil {
		t.Fatal("consumeImportFinished(broken payload) error = nil, want error")
	}
}
