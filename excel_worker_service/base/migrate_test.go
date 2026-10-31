package base

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationsAreOrderedAndIdempotentOnMySQL(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "worker_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db, err := OpenDB(config.MySQLDSN)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupWorkerMigrationState(t, db)
		_ = db.Close()
	})
	cleanupWorkerMigrationState(t, db)

	for round := 1; round <= 2; round++ {
		if err := MigrateUp(db); err != nil {
			t.Fatalf("round %d first MigrateUp() error = %v", round, err)
		}
		if !workerBootstrapTableExists(t, db) {
			t.Fatalf("round %d: bootstrap table missing after MigrateUp()", round)
		}
		if err := MigrateUp(db); err != nil {
			t.Fatalf("round %d repeated MigrateUp() error = %v", round, err)
		}
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

func cleanupWorkerMigrationState(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DROP TABLE IF EXISTS `excel_worker_bootstrap`",
		"DROP TABLE IF EXISTS `schema_migrations`",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("clean worker migration state: %v", err)
		}
	}
}
