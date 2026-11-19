package base

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"excel-libs/testmysql"
)

// 2026-11-18 数据事故护栏（规则见 excel-libs/testmysql）：本测试会执行迁移复位
// （DROP 指定表 + MigrateUp/MigrateDown 重放回滚）——只允许落在独立测试库上。
// configs 里的连接串指向共享 dev 库 excel_worker（live stack 正在使用），必须先经
// TestDSN 改写为 excel_worker_test。
func TestMigrationsAreOrderedAndIdempotentOnMySQL(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db := testmysql.OpenTestDB(t, testmysql.TestDSN(config.MySQLDSN))
	t.Cleanup(func() {
		cleanupWorkerMigrationState(t, db)
		_ = db.Close()
	})
	cleanupWorkerMigrationState(t, db)

	for round := 1; round <= 2; round++ {
		testmysql.MustRequireTestDatabase(t, db, "migration replay")
		if err := MigrateUp(db); err != nil {
			t.Fatalf("round %d first MigrateUp() error = %v", round, err)
		}
		if !workerBootstrapTableExists(t, db) {
			t.Fatalf("round %d: bootstrap table missing after MigrateUp()", round)
		}
		if err := MigrateUp(db); err != nil {
			t.Fatalf("round %d repeated MigrateUp() error = %v", round, err)
		}
		testmysql.MustRequireTestDatabase(t, db, "migration rollback")
		if err := MigrateDown(db); err != nil {
			t.Fatalf("round %d first MigrateDown() error = %v", round, err)
		}
		if workerBootstrapTableExists(t, db) {
			t.Fatalf("round %d: bootstrap table still exists after MigrateDown()", round)
		}
		if err := MigrateDown(db); err != nil {
			t.Fatalf("round %d repeated MigrateDown() error = %v", round, err)
		}
	}
}

func workerBootstrapTableExists(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var count int
	err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name = 'excel_worker_bootstrap'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query bootstrap table: %v", err)
	}
	return count == 1
}

// cleanupWorkerMigrationState drops the worker-owned migration artifacts so
// migrations replay from scratch.
//
// 2026-11-18 数据事故护栏（规则见 excel-libs/testmysql）：清库复位执行前硬校验连接
// 所在库必须以 _test 结尾（excel_worker_test）；连到共享库时立即失败。
func cleanupWorkerMigrationState(t *testing.T, db *sql.DB) {
	t.Helper()
	testmysql.MustRequireTestDatabase(t, db, "clean worker migration state")
	for _, statement := range []string{
		"DROP TABLE IF EXISTS `excel_worker_bootstrap`",
		"DROP TABLE IF EXISTS `excel_scheduler_tasks`",
		"DROP TABLE IF EXISTS `schema_migrations`",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("clean worker migration state: %v", err)
		}
	}
}
