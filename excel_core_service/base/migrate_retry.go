// base/migrate_retry.go：测试迁移执行入口的锁等待重试包装（仅测试调用，生产不重试）。
//
// golang-migrate 的 MySQL 驱动硬编码 GET_LOCK(?, 10)：共享 mysqld 高负载下偶发
// 「can't acquire lock」（database.ErrLocked）与 MySQL 1205。测试入口通过
// MigrateUpWithRetry/MigrateDownWithRetry 指数退避重试（2s/4s/8s，初始尝试后再重试 3 次）；
// 重试耗尽后原样上抛最后一次错误，绝不静默吞错。生产 main.go 仍调用不带重试的
// MigrateUp/MigrateDown，生产行为不变；不改 golang-migrate 驱动本身。
package base

import (
	"database/sql"
	"errors"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database"
)

// mysqlLockWaitTimeout is MySQL ER_LOCK_WAIT_TIMEOUT (1205), which surfaces when
// a migration statement loses a row-lock race under shared-mysqld load.
const mysqlLockWaitTimeout = 1205

// migrateMaxRetries is the number of retries after the initial attempt.
const migrateMaxRetries = 3

// migrateRetryBaseBackoff is the base interval of the exponential backoff
// (2s / 4s / 8s for retries 1..3).
const migrateRetryBaseBackoff = 2 * time.Second

// migrateRetryDelayDefault returns the wait before the given 1-based retry.
func migrateRetryDelayDefault(retry int) time.Duration {
	return migrateRetryBaseBackoff << (retry - 1)
}

// migrateRetryDelay is overridable so unit tests can collapse the backoff to zero.
var migrateRetryDelay = migrateRetryDelayDefault

func withMigrationRetry(run func() error) error {
	var lastErr error
	for attempt := 0; attempt <= migrateMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(migrateRetryDelay(attempt))
		}
		lastErr = run()
		if !isMigrationLockError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func isMigrationLockError(err error) bool {
	if errors.Is(err, migratedb.ErrLocked) || errors.Is(err, migrate.ErrLocked) {
		return true
	}
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlLockWaitTimeout
}

// MigrateUpWithRetry applies all pending core migrations, retrying lock-acquisition
// failures with exponential backoff. Test entry point only.
func MigrateUpWithRetry(db *sql.DB) error {
	return withMigrationRetry(func() error { return MigrateUp(db) })
}

// MigrateDownWithRetry reverts all applied core migrations, retrying lock-acquisition
// failures with exponential backoff. Test entry point only.
func MigrateDownWithRetry(db *sql.DB) error {
	return withMigrationRetry(func() error { return MigrateDown(db) })
}
