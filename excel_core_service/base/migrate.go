package base

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var coreMigrations embed.FS

// MigrateUp applies all pending core migrations.
func MigrateUp(db *sql.DB) error {
	migrator, closeSource, err := newCoreMigrator(db)
	if err != nil {
		return err
	}
	defer closeSource()

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate core up: %w", err)
	}
	return nil
}

// MigrateDown reverts all applied core migrations.
func MigrateDown(db *sql.DB) error {
	migrator, closeSource, err := newCoreMigrator(db)
	if err != nil {
		return err
	}
	defer closeSource()

	if err := migrator.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate core down: %w", err)
	}
	return nil
}

func newCoreMigrator(db *sql.DB) (*migrate.Migrate, func(), error) {
	sourceDriver, err := iofs.New(coreMigrations, "migrations")
	if err != nil {
		return nil, func() {}, fmt.Errorf("load core migrations: %w", err)
	}
	databaseDriver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		_ = sourceDriver.Close()
		return nil, func() {}, fmt.Errorf("create core migration database driver: %w", err)
	}
	statementDriver := &migrationStatementDriver{
		Driver: databaseDriver,
		db:     db,
	}
	migrator, err := migrate.NewWithInstance("iofs", sourceDriver, "mysql", statementDriver)
	if err != nil {
		_ = sourceDriver.Close()
		return nil, func() {}, fmt.Errorf("create core migrator: %w", err)
	}
	return migrator, func() { _ = sourceDriver.Close() }, nil
}

// migrationStatementDriver keeps golang-migrate's MySQL locking/version behavior
// while allowing one versioned file to contain multiple top-level SQL statements.
// WithInstance cannot enable the driver's multiStatements DSN option after the
// application pool has already been opened.
type migrationStatementDriver struct {
	migratedb.Driver
	db *sql.DB
}

func (driver *migrationStatementDriver) Run(migration io.Reader) error {
	script, err := io.ReadAll(migration)
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}
	for _, statement := range splitSQLStatements(string(script)) {
		if _, err := driver.db.Exec(statement); err != nil {
			return fmt.Errorf("execute migration statement: %w", err)
		}
	}
	return nil
}

func splitSQLStatements(script string) []string {
	var statements []string
	start := 0
	var quote byte
	var lineComment, blockComment bool

	for index := 0; index < len(script); index++ {
		current := script[index]
		if lineComment {
			if current == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if current == '*' && index+1 < len(script) && script[index+1] == '/' {
				blockComment = false
				index++
			}
			continue
		}
		if quote != 0 {
			if current == '\\' && quote != '`' && index+1 < len(script) {
				index++
				continue
			}
			if current == quote {
				if index+1 < len(script) && script[index+1] == quote {
					index++
					continue
				}
				quote = 0
			}
			continue
		}

		switch current {
		case '\'', '"', '`':
			quote = current
		case '#':
			lineComment = true
		case '-':
			if index+1 < len(script) && script[index+1] == '-' &&
				(index+2 == len(script) || script[index+2] == ' ' || script[index+2] == '\t' || script[index+2] == '\n') {
				lineComment = true
				index++
			}
		case '/':
			if index+1 < len(script) && script[index+1] == '*' {
				blockComment = true
				index++
			}
		case ';':
			statement := strings.TrimSpace(script[start:index])
			if statement != "" {
				statements = append(statements, statement)
			}
			start = index + 1
		}
	}

	statement := strings.TrimSpace(script[start:])
	if statement != "" {
		statements = append(statements, statement)
	}
	return statements
}
