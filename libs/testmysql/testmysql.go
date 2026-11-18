// Package testmysql 提供核心（excel_core_service）与 worker（excel_worker_service）
// 后端测试共用的 MySQL 测试库隔离护栏。
//
// # 背景：2026-11-18 共享库数据事故
//
// 2026-11-18 01:40，core 服务的集成测试复位路径（逐表 DROP 后重放迁移）在共享 dev 库
// excel_core / excel_worker（live stack 正在使用的库）上执行，把其中的表全部清空
// （binlog.000026 逐表 DROP 指纹）。本包是防止事故复发的硬护栏，规则如下：
//
//  1. 测试一律使用独立测试库 excel_core_test / excel_worker_test（共享库名加 _test
//     后缀的同名测试库）。测试框架在测试库内自管表：缺表时 MigrateUp 建表、用例间
//     DELETE 复位、结束后不复原（测试库无需保留数据）。
//  2. 任何指向非 _test 后缀库名的测试连接在 Open 阶段即硬失败（报错含库名与原因），
//     绝不建立连接。
//  3. 任何清库/迁移复位 helper（DROP TABLE、逐表清空、迁移重放）执行前必须先过
//     RequireTestDatabase：从连接上 SELECT DATABASE() 读取实际库名并做后缀硬校验，
//     违反时立即失败并给出清晰报错。DSN 校验 + 连接级校验双保险，防止改错连接串
//     后静默清库。
//  4. 测试库本身不存在时，Open 返回一次性建库 SQL 提示（CREATE DATABASE +
//     GRANT），绝不自动 DROP / CREATE DATABASE——建库由人工执行。
//  5. 唯一允许连接共享库的例外是只读验收检查：scripts/verify-test-db-isolation.ps1
//     与 excel_core_service/internal/testisolation 的快照对比测试（仅 SHOW/SELECT
//     计数），用于在 live stack 运行状态下证明全量测试前后共享库数据不变。
//
// 用法（服务测试基建）：
//
//	// 既有指向共享库的测试 DSN 改经 TestDSN 改写为测试库：
//	var testDSN = testmysql.TestDSN("excel:excel123@tcp(127.0.0.1:3306)/excel_core?...")
//	// 测试内开连接（后缀校验 + 缺库提示 + 连接池参数与 base.OpenDB 一致）：
//	db := testmysql.OpenTestDB(t, testDSN)
//	// 清库/迁移复位 helper 的第一行：
//	testmysql.MustRequireTestDatabase(t, db, "dropAllTables")
//
// 从服务 configs（*_service_config.yaml 的 mysql_dsn）解析出的连接串同样先过
// TestDSN 再交给 OpenTestDB，配置路径与硬编码路径口径一致。
package testmysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// TestDatabaseSuffix 是测试库唯一允许的库名后缀（excel_core_test / excel_worker_test）。
const TestDatabaseSuffix = "_test"

// TestSchemaName 把裸 schema 名改写为对应测试库 schema 名（与 TestDSN 同一改写规则）。
// 用于 SQL 里跨库全限定引用（如 worker 测试访问 core 侧 tracking/users 表）——
// 生产代码经可覆盖变量引用 CoreSchemaName，测试把它指到 excel_core_test。
func TestSchemaName(schemaName string) string {
	if schemaName == "" || strings.HasSuffix(schemaName, TestDatabaseSuffix) {
		return schemaName
	}
	return schemaName + TestDatabaseSuffix
}

// TestDSN 把 dsn 指向的库名改写为对应的测试库（<name>_test）。库名已带 _test 后缀
// 或无法解析时原样返回——后者交由 Open 的硬校验兜底报错。服务测试基建统一经由
// 本函数从「共享库连接串」派生「测试库连接串」，禁止手工另写一套测试库名。
func TestDSN(dsn string) string {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return dsn
	}
	config.DBName = TestSchemaName(config.DBName)
	return config.FormatDSN()
}

// ValidateTestDatabase 是全仓唯一的测试库后缀硬规则：库名必须以 _test 结尾。
// 违反时返回的错误包含库名与「测试库后缀校验失败」原因，供所有清库/迁移复位
// helper 与测试连接入口统一引用。
func ValidateTestDatabase(dbName string) error {
	if dbName == "" {
		return fmt.Errorf(
			"测试库后缀校验失败：目标库名为空，拒绝执行清库/迁移复位操作（2026-11-18 共享库数据事故护栏：只允许操作以 %s 结尾的测试库 excel_core_test/excel_worker_test，共享 dev 库正被 live stack 使用）",
			TestDatabaseSuffix)
	}
	if !strings.HasSuffix(dbName, TestDatabaseSuffix) {
		return fmt.Errorf(
			"测试库后缀校验失败：目标库 %q 不带 %s 后缀，拒绝执行清库/迁移复位操作（2026-11-18 共享库数据事故护栏：该库名可能是 live stack 正在使用的共享 dev 库 excel_core/excel_worker，测试只允许使用 *_test 测试库）",
			dbName, TestDatabaseSuffix)
	}
	return nil
}

// Open 打开一个指向测试库的受护栏连接池。dsn 的库名不带 _test 后缀时硬失败
// （不建立任何连接）；测试库不存在时返回带一次性建库 SQL 提示的错误。连接池
// 参数与 base.OpenDB 保持一致。
func Open(dsn string) (*sql.DB, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("测试库后缀校验失败：无法解析 MySQL DSN：%w", err)
	}
	if err := ValidateTestDatabase(config.DBName); err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		var mysqlErr *mysql.MySQLError
		// 1049 = Unknown database；1044 = Access denied to database——应用账号对
		// 未授权/不存在的库，MySQL 8 会以 1044 掩盖库是否存在，两种都按「缺库或缺
		// 授权」给出一次性建库提示，而不是自动建库。
		if errors.As(err, &mysqlErr) && (mysqlErr.Number == 1049 || mysqlErr.Number == 1044) {
			return nil, fmt.Errorf(
				"测试库 %q 不存在或应用账号未授权。请人工执行一次性建库 SQL 后重试（切勿对共享库 excel_core/excel_worker 执行任何 DDL/DML）：\nCREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;\nGRANT ALL PRIVILEGES ON `%s`.* TO 'excel'@'127.0.0.1';\nFLUSH PRIVILEGES;",
				config.DBName, config.DBName, config.DBName)
		}
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return db, nil
}

// OpenTestDB 是测试基建使用的 testing.TB 门面：护栏失败时直接 Fatal，杜绝测试
// 在非测试库上继续执行。
func OpenTestDB(t testing.TB, dsn string) *sql.DB {
	t.Helper()
	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("testmysql.OpenTestDB() error = %v", err)
	}
	return db
}

// RequireTestDatabase 在执行任何清库/迁移复位操作前校验「连接实际所在库」：
// SELECT DATABASE() 读取库名并做后缀硬校验。label 标注调用方（如 dropAllTables），
// 便于报错定位。这是连接级兜底——即使 DSN 改写被绕过，实际连到共享库也会在此
// 硬失败。
func RequireTestDatabase(db *sql.DB, label string) error {
	if db == nil {
		return fmt.Errorf("%s: 测试库后缀校验失败：数据库连接为空", label)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var name string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		return fmt.Errorf("%s: 测试库后缀校验失败：读取当前数据库名：%w", label, err)
	}
	if err := ValidateTestDatabase(name); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// MustRequireTestDatabase 是 RequireTestDatabase 的 testing.TB 门面，供测试文件
// 中的清库/迁移复位 helper 第一行调用。
func MustRequireTestDatabase(t testing.TB, db *sql.DB, label string) {
	t.Helper()
	if err := RequireTestDatabase(db, label); err != nil {
		t.Fatal(err)
	}
}
