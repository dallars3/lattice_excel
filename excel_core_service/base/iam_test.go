package base

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"excel-libs/testmysql"
)

// 评审整改回归（acceptance-defect-remediation 双轴评审第 1 项）：
// EnsureDefaultEnterprise 的所有错误分支必须返回**原 user**（保持孤立态），
// 绝不返回零值——否则 login/get_me 的「失败仅告警不阻断」路径会拿零值用户
// 建会话/回响应（空 user_id 会话，违反 CODING_STANDARDS §3）。
// 失败注入方式：预置与补建主键相同的企业行，令企业 INSERT 撞主键（1062），
// 事务回滚后断言返回值与库内状态均为原始孤立形态。
func TestEnsureDefaultEnterpriseReturnsOriginalUserOnError(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "configs", "core_service_config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	db := testmysql.OpenTestDB(t, testmysql.TestDSN(config.MySQLDSN))
	t.Cleanup(func() { _ = db.Close() })
	if err := MigrateUpWithRetry(db); err != nil {
		t.Fatalf("MigrateUpWithRetry() error = %v", err)
	}
	testmysql.MustRequireTestDatabase(t, db, "EnsureDefaultEnterprise contract test")

	ctx := context.Background()
	const (
		orphanID  = "01J000000000000000000AD101"
		collideID = "01J000000000000000000AD102"
	)
	createdAt := time.Now().UTC().UnixMilli()
	seedOrphanIAMUser(t, db, orphanID, "Orphan_Backfill", createdAt)
	seedIAMEnterprise(t, db, collideID, "占用主键的企业", orphanID, createdAt)
	t.Cleanup(func() { cleanupIAMFixture(t, db, orphanID, collideID) })

	repository := NewIAMRepository(db)
	user := IAMUser{ID: orphanID, Username: "Orphan_Backfill", Internal: true, CreatedAt: createdAt}
	enterprise := IAMEnterprise{
		ID:          collideID,
		Name:        "Orphan_Backfill 的企业",
		OwnerID:     orphanID,
		MemberCount: 1,
		CreatedAt:   createdAt,
	}

	updated, created, err := repository.EnsureDefaultEnterprise(ctx, user, enterprise)
	if err == nil {
		t.Fatalf("EnsureDefaultEnterprise() created=%v error=nil, want duplicate-key error", created)
	}
	if created {
		t.Fatal("EnsureDefaultEnterprise() created=true on error, want false")
	}
	if updated.ID != user.ID || updated.Username != user.Username ||
		updated.EnterpriseID != "" || !updated.Internal {
		t.Fatalf(
			"error branch returned mutated/zero user: %+v, want original %+v",
			updated,
			user,
		)
	}

	// 库内保持孤立态：事务已回滚——无成员行、无企业归属。
	var memberCount int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM excel_iam_members WHERE user_id = ?", orphanID,
	).Scan(&memberCount); err != nil {
		t.Fatalf("count backfill memberships: %v", err)
	}
	if memberCount != 0 {
		t.Fatalf("memberships = %d, want 0 (transaction rolled back)", memberCount)
	}
	var enterpriseID string
	if err := db.QueryRowContext(ctx,
		"SELECT COALESCE(enterprise_id, '') FROM excel_iam_users WHERE id = ?", orphanID,
	).Scan(&enterpriseID); err != nil {
		t.Fatalf("read orphan enterprise_id: %v", err)
	}
	if enterpriseID != "" {
		t.Fatalf("orphan enterprise_id = %q, want empty (isolated state kept)", enterpriseID)
	}
}

// seedOrphanIAMUser inserts an internal user without enterprise membership
// （票 02 前注册的存量孤立账号形态）。
func seedOrphanIAMUser(t *testing.T, db *sql.DB, userID, username string, createdAt int64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_iam_users
			(id, username, display_name, avatar_uri, password_hash,
			 enterprise_id, internal, link_base_id, link_token, link_role, created_at)
		VALUES (?, ?, ?, '', '$2a$10$stubbedhashstubbedhashstubbedhashstubbedhashstub',
		        NULL, 1, NULL, NULL, NULL, ?)
	`, userID, username, username, createdAt)
	if err != nil {
		t.Fatalf("seed orphan IAM user: %v", err)
	}
}

// seedIAMEnterprise inserts an enterprise row occupying a chosen primary key,
// forcing the backfill's enterprise INSERT to collide（失败注入缝）。
func seedIAMEnterprise(
	t *testing.T,
	db *sql.DB,
	enterpriseID, name, ownerID string,
	createdAt int64,
) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO excel_iam_enterprises (id, name, owner_id, member_count, created_at)
		VALUES (?, ?, ?, 1, ?)
	`, enterpriseID, name, ownerID, createdAt)
	if err != nil {
		t.Fatalf("seed IAM enterprise: %v", err)
	}
}

// cleanupIAMFixture removes the seeded rows so the shared test database stays
// neutral for the following suites（测试库用例间 DELETE 复位口径）。
func cleanupIAMFixture(t *testing.T, db *sql.DB, userID, enterpriseID string) {
	t.Helper()
	testmysql.MustRequireTestDatabase(t, db, "cleanup IAM fixture")
	for _, fixture := range []struct {
		query     string
		arguments []any
	}{
		{
			"DELETE FROM excel_iam_members WHERE user_id = ? OR enterprise_id = ?",
			[]any{userID, enterpriseID},
		},
		{"DELETE FROM excel_iam_enterprises WHERE id = ?", []any{enterpriseID}},
		{"DELETE FROM excel_iam_users WHERE id = ?", []any{userID}},
	} {
		if _, err := db.ExecContext(context.Background(), fixture.query, fixture.arguments...); err != nil {
			t.Fatalf("cleanup IAM fixture: %v", err)
		}
	}
}
