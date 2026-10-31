package base

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var workerMigrations embed.FS

// MigrateUp applies all pending worker migrations.
func MigrateUp(db *sql.DB) error {
	migrator, closeSource, err := newWorkerMigrator(db)
	if err != nil {
		return err
	}
	defer closeSource()

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate worker up: %w", err)
	}
	return nil
}

// MigrateDown reverts all applied worker migrations.
func MigrateDown(db *sql.DB) error {
	migrator, closeSource, err := newWorkerMigrator(db)
	if err != nil {
		return err
	}
	defer closeSource()

	if err := migrator.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate worker down: %w", err)
	}
	return nil
}

func newWorkerMigrator(db *sql.DB) (*migrate.Migrate, func(), error) {
	sourceDriver, err := iofs.New(workerMigrations, "migrations")
	if err != nil {
		return nil, func() {}, fmt.Errorf("load worker migrations: %w", err)
	}
	databaseDriver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		_ = sourceDriver.Close()
		return nil, func() {}, fmt.Errorf("create worker migration database driver: %w", err)
	}
	migrator, err := migrate.NewWithInstance("iofs", sourceDriver, "mysql", databaseDriver)
	if err != nil {
		_ = sourceDriver.Close()
		return nil, func() {}, fmt.Errorf("create worker migrator: %w", err)
	}
	return migrator, func() { _ = sourceDriver.Close() }, nil
}
