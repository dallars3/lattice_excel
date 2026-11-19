package base

import (
	"errors"
	"fmt"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database"
)

// withZeroRetryDelay collapses the backoff schedule to zero for attempt-count
// assertions, restoring the default afterwards.
func withZeroRetryDelay(t *testing.T) {
	t.Helper()
	original := migrateRetryDelay
	migrateRetryDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() { migrateRetryDelay = original })
}

func TestWithMigrationRetrySucceedsAfterLockErrors(t *testing.T) {
	withZeroRetryDelay(t)
	attempts := 0
	err := withMigrationRetry(func() error {
		attempts++
		if attempts <= 2 {
			return fmt.Errorf("migrate worker up: %w", migratedb.ErrLocked)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withMigrationRetry() error = %v, want nil after lock retries", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (2 lock failures + success)", attempts)
	}
}

func TestWithMigrationRetryDoesNotRetryOtherErrors(t *testing.T) {
	withZeroRetryDelay(t)
	attempts := 0
	wantErr := errors.New("syntax error in migration")
	err := withMigrationRetry(func() error {
		attempts++
		return wantErr
	})
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (non-lock error must not retry)", attempts)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want original error returned as-is", err)
	}
}

func TestWithMigrationRetryExhaustsRetriesAndSurfacesLastError(t *testing.T) {
	withZeroRetryDelay(t)
	attempts := 0
	var lastAttemptErr error
	err := withMigrationRetry(func() error {
		attempts++
		lastAttemptErr = fmt.Errorf("migrate worker up: %w", migratedb.ErrLocked)
		return lastAttemptErr
	})
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4 (initial attempt + 3 retries)", attempts)
	}
	if err == nil {
		t.Fatal("error = nil, want last lock error surfaced after retry exhaustion")
	}
	if !errors.Is(err, migratedb.ErrLocked) {
		t.Fatalf("error = %v, want wrapped database.ErrLocked preserved", err)
	}
	if err.Error() != lastAttemptErr.Error() {
		t.Fatalf("error = %q, want last attempt error verbatim %q", err, lastAttemptErr)
	}
}

func TestIsMigrationLockError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"driver lock timeout", migratedb.ErrLocked, true},
		{"wrapped driver lock timeout", fmt.Errorf("migrate worker up: %w", migratedb.ErrLocked), true},
		{"migrate package lock", migrate.ErrLocked, true},
		{"mysql lock wait timeout 1205", fmt.Errorf("execute migration statement: %w", &mysqlDriver.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded; try restarting transaction"}), true},
		{"mysql deadlock 1213", &mysqlDriver.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock"}, false},
		{"unrelated mysql error", &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry"}, false},
		{"plain error", errors.New("connection refused"), false},
		{"nil error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMigrationLockError(tc.err); got != tc.want {
				t.Fatalf("isMigrationLockError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestMigrateRetryDelayDefaultExponential(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for retry, delay := range want {
		if got := migrateRetryDelayDefault(retry + 1); got != delay {
			t.Fatalf("migrateRetryDelayDefault(%d) = %v, want %v", retry+1, got, delay)
		}
	}
}
