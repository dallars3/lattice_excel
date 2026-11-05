package base

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationsAreOrderedAndIdempotentOnMySQL(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db, err := OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCoreMigrationState(t, db)
		_ = db.Close()
	})
	cleanupCoreMigrationState(t, db)

	for round := 1; round <= 2; round++ {
		if err := MigrateUp(db); err != nil {
			t.Fatalf("round %d first MigrateUp() error = %v", round, err)
		}
		if !coreBootstrapTableExists(t, db) {
			t.Fatalf("round %d: bootstrap table missing after MigrateUp()", round)
		}
		if err := MigrateUp(db); err != nil {
			t.Fatalf("round %d repeated MigrateUp() error = %v", round, err)
		}
		if err := MigrateDown(db); err != nil {
			t.Fatalf("round %d first MigrateDown() error = %v", round, err)
		}
		if coreBootstrapTableExists(t, db) {
			t.Fatalf("round %d: bootstrap table still exists after MigrateDown()", round)
		}
		if err := MigrateDown(db); err != nil {
			t.Fatalf("round %d repeated MigrateDown() error = %v", round, err)
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
func dropAllTables(t *testing.T, db *sql.DB, label string) {
	t.Helper()
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
