package testmysql_test

import (
	"strings"
	"testing"

	"excel-libs/testmysql"
)

// guardTestSharedCoreDSN 与 core 服务测试文件中的既有连接串一致：它指向共享 dev 库
// excel_core，本文件只把它作为 TestDSN 的输入（绝不直接建立连接）。
const guardTestSharedCoreDSN = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"

// guardTestMissingDBDSN 指向一个不存在的测试库名，用于验证缺库时的一次性建库提示。
// 连接该库名只会收到 MySQL 1049（Unknown database），不会创建或修改任何库。
const guardTestMissingDBDSN = "excel:excel123@tcp(127.0.0.1:3306)/excel_guard_missing_probe_test?charset=utf8mb4"

func TestValidateTestDatabaseRejectsSharedDatabases(t *testing.T) {
	for _, name := range []string{
		"excel_core",
		"excel_worker",
		"",
		"excel_core_TEST",
		"excel_core_test_backup",
	} {
		err := testmysql.ValidateTestDatabase(name)
		if err == nil {
			t.Fatalf("ValidateTestDatabase(%q) = nil, want error", name)
		}
		if !strings.Contains(err.Error(), "测试库后缀校验失败") {
			t.Fatalf("ValidateTestDatabase(%q) error = %v, want 测试库后缀校验失败 in message", name, err)
		}
		if !strings.Contains(err.Error(), name) && name != "" {
			t.Fatalf("ValidateTestDatabase(%q) error = %v, want database name in message", name, err)
		}
	}
}

func TestValidateTestDatabaseAcceptsTestDatabases(t *testing.T) {
	for _, name := range []string{"excel_core_test", "excel_worker_test"} {
		if err := testmysql.ValidateTestDatabase(name); err != nil {
			t.Fatalf("ValidateTestDatabase(%q) error = %v, want nil", name, err)
		}
	}
}

func TestTestDSNAppendsTestSuffix(t *testing.T) {
	dsn := testmysql.TestDSN(guardTestSharedCoreDSN)
	if !strings.Contains(dsn, "/excel_core_test?") {
		t.Fatalf("TestDSN() = %q, want database rewritten to excel_core_test", dsn)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") || !strings.Contains(dsn, "parseTime=true") {
		t.Fatalf("TestDSN() = %q, want original DSN parameters preserved", dsn)
	}
}

func TestTestSchemaNameAppendsTestSuffix(t *testing.T) {
	if name := testmysql.TestSchemaName("excel_core"); name != "excel_core_test" {
		t.Fatalf("TestSchemaName(excel_core) = %q, want excel_core_test", name)
	}
	if name := testmysql.TestSchemaName("excel_worker_test"); name != "excel_worker_test" {
		t.Fatalf("TestSchemaName(excel_worker_test) = %q, want unchanged", name)
	}
	if name := testmysql.TestSchemaName(""); name != "" {
		t.Fatalf("TestSchemaName(empty) = %q, want empty", name)
	}
}

func TestTestDSNKeepsExistingTestSuffix(t *testing.T) {
	testDSN := "excel:excel123@tcp(127.0.0.1:3306)/excel_core_test?charset=utf8mb4"
	if dsn := testmysql.TestDSN(testDSN); dsn != testDSN {
		t.Fatalf("TestDSN() = %q, want %q unchanged", dsn, testDSN)
	}
}

func TestOpenRejectsSharedDatabaseDSN(t *testing.T) {
	_, err := testmysql.Open(guardTestSharedCoreDSN)
	if err == nil {
		t.Fatal("Open(shared DSN) = nil error, want hard failure before any connection")
	}
	if !strings.Contains(err.Error(), "测试库后缀校验失败") {
		t.Fatalf("Open(shared DSN) error = %v, want 测试库后缀校验失败 in message", err)
	}
	if !strings.Contains(err.Error(), "excel_core") {
		t.Fatalf("Open(shared DSN) error = %v, want database name in message", err)
	}
}

func TestOpenMissingTestDatabaseSuggestsCreateDatabase(t *testing.T) {
	_, err := testmysql.Open(guardTestMissingDBDSN)
	if err == nil {
		t.Fatal("Open(missing test database) = nil error, want create-database hint")
	}
	if !strings.Contains(err.Error(), "CREATE DATABASE") {
		t.Fatalf("Open(missing test database) error = %v, want one-shot CREATE DATABASE hint", err)
	}
	if strings.Contains(err.Error(), "IDENTIFIED BY") || strings.Contains(err.Error(), "PASSWORD(") {
		t.Fatal("Open(missing test database) error must never embed credentials")
	}
}

func TestOpenTestDBOpensGuardedTestPool(t *testing.T) {
	if testing.Short() {
		t.Skip("需要本机 MySQL（127.0.0.1:3306）")
	}
	db := testmysql.OpenTestDB(t, testmysql.TestDSN(guardTestSharedCoreDSN))
	defer func() { _ = db.Close() }()

	var name string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatalf("SELECT DATABASE() error = %v", err)
	}
	if name != "excel_core_test" {
		t.Fatalf("SELECT DATABASE() = %q, want excel_core_test", name)
	}
}

func TestMustRequireTestDatabaseAcceptsGuardedPool(t *testing.T) {
	if testing.Short() {
		t.Skip("需要本机 MySQL（127.0.0.1:3306）")
	}
	db := testmysql.OpenTestDB(t, testmysql.TestDSN(guardTestSharedCoreDSN))
	defer func() { _ = db.Close() }()

	testmysql.MustRequireTestDatabase(t, db, "guard self check")
}
