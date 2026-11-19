package base

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"excel-libs/testmysql"
)

// 2026-11-18 数据事故护栏（规则见 excel-libs/testmysql）：本测试会执行逐表 DROP 与
// MigrateDownWithRetry 全量回滚——迁移复位操作只允许落在独立测试库上。configs 里的连接串指向
// 共享 dev 库 excel_core（live stack 正在使用），必须先经 TestDSN 改写为 excel_core_test。
func TestMigrationsAreOrderedAndIdempotentOnMySQL(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db := testmysql.OpenTestDB(t, testmysql.TestDSN(config.MySQLDSN))
	t.Cleanup(func() {
		cleanupCoreMigrationState(t, db)
		_ = db.Close()
	})
	cleanupCoreMigrationState(t, db)

	for round := 1; round <= 2; round++ {
		testmysql.MustRequireTestDatabase(t, db, "migration replay")
		if err := MigrateUpWithRetry(db); err != nil {
			t.Fatalf("round %d first MigrateUpWithRetry() error = %v", round, err)
		}
		if !coreBootstrapTableExists(t, db) {
			t.Fatalf("round %d: bootstrap table missing after MigrateUpWithRetry()", round)
		}
		if err := MigrateUpWithRetry(db); err != nil {
			t.Fatalf("round %d repeated MigrateUpWithRetry() error = %v", round, err)
		}
		testmysql.MustRequireTestDatabase(t, db, "migration rollback")
		if err := MigrateDownWithRetry(db); err != nil {
			t.Fatalf("round %d first MigrateDownWithRetry() error = %v", round, err)
		}
		if coreBootstrapTableExists(t, db) {
			t.Fatalf("round %d: bootstrap table still exists after MigrateDownWithRetry()", round)
		}
		if err := MigrateDownWithRetry(db); err != nil {
			t.Fatalf("round %d repeated MigrateDownWithRetry() error = %v", round, err)
		}
	}
}

func coreBootstrapTableExists(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var count int
	err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name = 'excel_core_bootstrap'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query bootstrap table: %v", err)
	}
	return count == 1
}

func cleanupCoreMigrationState(t *testing.T, db *sql.DB) {
	t.Helper()
	dropAllTables(t, db, "clean core migration state")
}

// dropAllTables removes every table in the current database so migrations can
// replay from scratch. Migrations 000015/000016/000018 ALTER pre-existing tables,
// so a partial drop leaves duplicate columns and a dirty migration version.
//
// 2026-11-18 数据事故护栏（规则见 excel-libs/testmysql）：本 helper 逐表 DROP，
// 执行前硬校验连接所在库必须以 _test 结尾；连到共享 dev 库（excel_core/excel_worker）
// 时立即失败，绝不落任何 DDL。
func dropAllTables(t *testing.T, db *sql.DB, label string) {
	t.Helper()
	testmysql.MustRequireTestDatabase(t, db, label)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("%s: acquire connection: %v", label, err)
	}
	defer conn.Close()
	defer func() { _, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1") }()

	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("%s: disable foreign key checks: %v", label, err)
	}
	rows, err := conn.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		t.Fatalf("%s: list tables: %v", label, err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			t.Fatalf("%s: scan table name: %v", label, err)
		}
		names = append(names, name)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: iterate tables: %v", label, err)
	}
	for _, name := range names {
		if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS `"+name+"`"); err != nil {
			t.Fatalf("%s: drop table %s: %v", label, name, err)
		}
	}
}
