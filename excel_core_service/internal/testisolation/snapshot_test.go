// Package testisolation 承载 2026-11-18 数据事故护栏的验收证据：
// 在 live stack 运行状态下跑全量后端测试前后，对共享 dev 库
// （excel_core / excel_worker）做「表集合 + 逐表行数」快照对比，前后必须完全不变。
//
// 这是唯一允许连接共享库的测试——仅做只读检查（information_schema 与 SELECT COUNT），
// 不经过 testmysql 护栏（护栏会拒绝非 _test 后缀库名），也绝不写入。
//
// 本测试由环境变量门控，普通 `go test ./...` 默认 Skip：
//
//	AD01_SNAPSHOT_MODE=record  AD01_SNAPSHOT_FILE=<path>  → 记录快照到文件
//	AD01_SNAPSHOT_MODE=verify  AD01_SNAPSHOT_FILE=<path>  → 与文件比对，任何差异即失败
//
// 可重复执行的口径见 scripts/verify-test-db-isolation.ps1：record → 全量 go test
// （core+worker）→ verify。全绿且快照不变 = 测试库隔离护栏生效。
package testisolation

import (
	"database/sql"
	"encoding/json"
	"os"
	"sort"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// snapshotSources 是被保护的共享 dev 库连接串（live stack 正在使用）。只读用途，
// 与测试库隔离护栏互为对照：所有清库/迁移 helper 拒绝这些库名，本测试只读校验它们。
var snapshotSources = []string{
	"excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true",
	"excel:excel123@tcp(127.0.0.1:3306)/excel_worker?charset=utf8mb4&parseTime=true",
}

type tableSnapshot struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

type databaseSnapshot struct {
	Database string          `json:"database"`
	Tables   []tableSnapshot `json:"tables"`
}

func TestSharedDevDatabaseSnapshot(t *testing.T) {
	file := os.Getenv("AD01_SNAPSHOT_FILE")
	if file == "" {
		t.Skip("需要 AD01_SNAPSHOT_FILE（配合 scripts/verify-test-db-isolation.ps1 使用）")
	}
	mode := os.Getenv("AD01_SNAPSHOT_MODE")
	switch mode {
	case "record":
		snapshots := captureSnapshots(t)
		encoded, err := json.MarshalIndent(snapshots, "", "  ")
		if err != nil {
			t.Fatalf("marshal snapshot: %v", err)
		}
		if err := os.WriteFile(file, encoded, 0o644); err != nil {
			t.Fatalf("write snapshot %s: %v", file, err)
		}
		t.Logf("recorded %d databases into %s", len(snapshots), file)
	case "verify":
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read snapshot %s: %v", file, err)
		}
		var before []databaseSnapshot
		if err := json.Unmarshal(raw, &before); err != nil {
			t.Fatalf("decode snapshot %s: %v", file, err)
		}
		after := captureSnapshots(t)
		assertSnapshotsUnchanged(t, before, after)
	default:
		t.Skip("需要 AD01_SNAPSHOT_MODE=record|verify（普通测试运行跳过）")
	}
}

func captureSnapshots(t *testing.T) []databaseSnapshot {
	t.Helper()
	var snapshots []databaseSnapshot
	for _, dsn := range snapshotSources {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Fatalf("open snapshot source: %v", err)
		}
		snapshots = append(snapshots, captureDatabaseSnapshot(t, db))
		_ = db.Close()
	}
	return snapshots
}

func captureDatabaseSnapshot(t *testing.T, db *sql.DB) databaseSnapshot {
	t.Helper()
	var database string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatalf("SELECT DATABASE(): %v", err)
	}
	rows, err := db.Query(`
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`)
	if err != nil {
		t.Fatalf("list tables in %s: %v", database, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name in %s: %v", database, err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables in %s: %v", database, err)
	}
	sort.Strings(names)

	snapshot := databaseSnapshot{Database: database}
	for _, name := range names {
		var count int64
		// 逐表精确计数（information_schema.TABLE_ROWS 对 InnoDB 是估算值，不可用于断言）。
		if err := db.QueryRow("SELECT COUNT(*) FROM `" + name + "`").Scan(&count); err != nil {
			t.Fatalf("count rows of %s.%s: %v", database, name, err)
		}
		snapshot.Tables = append(snapshot.Tables, tableSnapshot{Name: name, Rows: count})
	}
	return snapshot
}

func assertSnapshotsUnchanged(t *testing.T, before, after []databaseSnapshot) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("snapshot database count = %d, want %d", len(after), len(before))
	}
	for index := range before {
		was, now := before[index], after[index]
		if was.Database != now.Database {
			t.Fatalf("snapshot[%d] database = %q, want %q", index, now.Database, was.Database)
		}
		if len(was.Tables) != len(now.Tables) {
			t.Fatalf("%s: table count %d -> %d（测试不得增删共享库表）",
				was.Database, len(was.Tables), len(now.Tables))
		}
		for i := range was.Tables {
			beforeTable, afterTable := was.Tables[i], now.Tables[i]
			if beforeTable.Name != afterTable.Name {
				t.Fatalf("%s: table[%d] name %q -> %q（测试不得增删共享库表）",
					was.Database, i, afterTable.Name, beforeTable.Name)
			}
			if beforeTable.Rows != afterTable.Rows {
				t.Errorf("%s.%s: row count %d -> %d（测试不得改写共享库数据）",
					was.Database, beforeTable.Name, beforeTable.Rows, afterTable.Rows)
			}
		}
	}
	// 用 Errorf 收集全部差异后再统一失败，便于一次性看清共享库被谁动了。
	if t.Failed() {
		t.Fatal("共享 dev 库快照对比发现差异：全量测试触碰了 live stack 正在使用的库")
	}
}
