package share

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"excel-libs/eventbus"
	"excel-libs/testmysql"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
)

// 共享 dev 库连接串经 TestDSN 改写为 excel_core_test（2026-11-18 清库事故护栏，
// 规则见 excel-libs/testmysql）；测试禁止直接连接共享库。
var shareTestMySQLDSN = testmysql.TestDSN("excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true")

const shareTestRedisAddr = "127.0.0.1:6379"

type shareBaseResolver map[string]string

func (resolver shareBaseResolver) EnterpriseID(_ context.Context, baseID string) (string, error) {
	return resolver[baseID], nil
}

// shareImportLock fakes the workspace import lock for CheckAccess tests.
type shareImportLock map[string]bool

func (fake shareImportLock) Importing(_ context.Context, baseID string) (bool, error) {
	return fake[baseID], nil
}

type shareMemberRoleProvider map[string]iamv1.MemberRole

// Role 按接缝契约返回：无成员行回 UNSPECIFIED+nil（非成员不是基础设施错误）；
// 票 03 起 share 对查询错误一律 fail-closed 拒绝，桩必须与真实适配器同形。
func (provider shareMemberRoleProvider) Role(_ context.Context, enterpriseID, userID string) (iamv1.MemberRole, error) {
	return provider[enterpriseID+"/"+userID], nil
}

type shareUserSpec struct {
	userID   string
	internal bool
}

type shareUserLookup map[string]shareUserSpec

func (lookup shareUserLookup) Lookup(_ context.Context, username string) (string, bool, error) {
	user, ok := lookup[username]
	if !ok {
		return "", false, sql.ErrNoRows
	}
	return user.userID, user.internal, nil
}

type shareBaseOwnerProvider map[string]string

func (provider shareBaseOwnerProvider) OwnerID(_ context.Context, baseID string) (string, error) {
	ownerID, ok := provider[baseID]
	if !ok {
		return "", sql.ErrNoRows
	}
	return ownerID, nil
}

// shareBaseNameProvider fakes the workspace Base-name seam for H-71 list tests.
type shareBaseNameProvider map[string]string

func (provider shareBaseNameProvider) BaseName(_ context.Context, baseID string) (string, error) {
	name, ok := provider[baseID]
	if !ok {
		return "", sql.ErrNoRows
	}
	return name, nil
}

// shareProfileProvider fakes the IAM display-identity seam for collaborator entries.
type shareProfileProvider map[string]*iamv1.User

func (provider shareProfileProvider) ProfilesByIDs(
	_ context.Context,
	userIDs []string,
) (map[string]*iamv1.User, error) {
	profiles := make(map[string]*iamv1.User, len(userIDs))
	for _, userID := range userIDs {
		if user, ok := provider[userID]; ok {
			profiles[userID] = user
		}
	}
	return profiles, nil
}

type shareTestHarness struct {
	client              sharev1.ShareServiceClient
	db                  *sql.DB
	baseResolver        shareBaseResolver
	baseNames           shareBaseNameProvider
	baseOwner           shareBaseOwnerProvider
	importLock          shareImportLock
	memberRole          shareMemberRoleProvider
	userLookup          shareUserLookup
	profiles            shareProfileProvider
	enterpriseDirectory shareEnterpriseDirectory
	permissionConfig    string
	events              *eventbus.MemoryBus
	forwarder           *bus.Forwarder
}

func TestPermissionPolicyReloadsOnNextRequestAfterMTimeChange(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID = "01J00000000000000000003001"
		userID = "01J00000000000000000003002"
	)
	harness.baseResolver[baseID] = "01J00000000000000000003003"
	if _, err := harness.db.ExecContext(ctx, `
		INSERT INTO excel_share_collaborators
			(base_id, user_id, role, internal, source, granted_at)
		VALUES (?, ?, 'viewer', 0, 'invite', ?)
	`, baseID, userID, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed collaborator: %v", err)
	}

	request := &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{UserId: userID, Internal: false},
		BaseId:    baseID,
		Action:    sharev1.Action_ACTION_MANAGE_SHARE,
	}
	before, err := harness.client.CheckAccess(ctx, request)
	if err != nil {
		t.Fatalf("CheckAccess(before reload) error = %v", err)
	}
	if before.GetAllow() {
		t.Fatal("CheckAccess(before reload) allow = true, want false")
	}

	writeSharePermissionPolicy(t, harness.permissionConfig, true)
	after, err := harness.client.CheckAccess(ctx, request)
	if err != nil {
		t.Fatalf("CheckAccess(after reload) error = %v", err)
	}
	if !after.GetAllow() {
		t.Fatal("CheckAccess(after reload) allow = false, want true")
	}
}

func newShareTestHarness(t *testing.T) *shareTestHarness {
	return newShareTestHarnessWithPolicy(t, false)
}

func newShareTestHarnessWithPolicy(
	t *testing.T,
	viewerCanManageShare bool,
) *shareTestHarness {
	t.Helper()

	db := testmysql.OpenTestDB(t, shareTestMySQLDSN)
	resetShareTestMigrationState(t, db)
	if err := base.MigrateUp(db); err != nil {
		_ = db.Close()
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetShareTestEvents(t, db)

	redisClient := goredis.NewClient(&goredis.Options{Addr: shareTestRedisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		_ = db.Close()
		_ = redisClient.Close()
		t.Fatalf("redis ping: %v", err)
	}
	_ = redisClient.FlushDB(context.Background()).Err()

	permissionConfig := filepath.Join(t.TempDir(), "permissions.json")
	writeSharePermissionPolicy(t, permissionConfig, viewerCanManageShare)
	baseResolver := shareBaseResolver{}
	baseNames := shareBaseNameProvider{}
	baseOwner := shareBaseOwnerProvider{}
	importLock := shareImportLock{}
	memberRoles := shareMemberRoleProvider{}
	userLookup := shareUserLookup{}
	profiles := shareProfileProvider{}
	enterpriseDirectory := shareEnterpriseDirectory{}
	events := eventbus.NewMemoryBus()
	outbox := bus.NewOutbox(base.NewBusStore(db))
	service, err := NewService(ServiceDeps{
		Repository:           base.NewShareRepository(db),
		Bases:                baseResolver,
		BaseNames:            baseNames,
		BaseOwner:            baseOwner,
		ImportLock:           importLock,
		Members:              memberRoles,
		UserLookup:           userLookup,
		Profiles:             profiles,
		EnterpriseDirectory:  enterpriseDirectory,
		Outbox:               outbox,
		PermissionConfigPath: permissionConfig,
	})
	if err != nil {
		_ = redisClient.Close()
		_ = db.Close()
		t.Fatalf("NewService() error = %v", err)
	}

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	Register(server, service)
	go func() {
		_ = server.Serve(listener)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		server.Stop()
		_ = listener.Close()
		_ = redisClient.Close()
		_ = db.Close()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
		resetShareTestData(t, db)
		_ = redisClient.Close()
		_ = db.Close()
	})
	return &shareTestHarness{
		client:              sharev1.NewShareServiceClient(conn),
		db:                  db,
		baseResolver:        baseResolver,
		baseNames:           baseNames,
		baseOwner:           baseOwner,
		importLock:          importLock,
		memberRole:          memberRoles,
		userLookup:          userLookup,
		profiles:            profiles,
		enterpriseDirectory: enterpriseDirectory,
		permissionConfig:    permissionConfig,
		events:              events,
		forwarder:           bus.NewForwarder(base.NewBusStore(db), events, bus.ForwarderConfig{}),
	}
}

func writeSharePermissionPolicy(t *testing.T, path string, viewerCanManageShare bool) {
	t.Helper()
	viewerActions := `"view", "comment"`
	if viewerCanManageShare {
		viewerActions += `, "manage_share"`
	}
	payload := `{
		"schema_version": 1,
		"roles": {
			"manager": {"name": "可管理", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "manage_structure", "comment", "export", "manage_share", "copy_base", "import"]},
			"editor": {"name": "可编辑", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "comment", "export"]},
			"filler": {"name": "可填写", "actions": ["view_own", "create_record", "edit_own", "comment"]},
			"viewer": {"name": "可查看", "actions": [` + viewerActions + `]}
		},
		"external": {"export_allowed": false}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write permission policy: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("set permission policy mtime: %v", err)
	}
}

func resetShareTestMigrationState(t *testing.T, db *sql.DB) {
	t.Helper()
	if !shareMigrationIsDirty(db) {
		return
	}
	dropAllTables(t, db, "reset share migration state")
}

// shareMigrationIsDirty reports whether schema_migrations holds a dirty version
// (a half-applied migration that golang-migrate refuses to advance past). A
// clean (or missing) version table can be brought up to date by MigrateUp
// directly, so the expensive drop-all-and-replay path is only taken on recovery.
func shareMigrationIsDirty(db *sql.DB) bool {
	var dirty bool
	err := db.QueryRowContext(context.Background(),
		"SELECT dirty FROM schema_migrations WHERE dirty = 1 LIMIT 1").Scan(&dirty)
	return err == nil && dirty
}

// dropAllTables removes every table in the current database so migrations can
// replay from scratch. Migrations 000015/000016/000018 ALTER pre-existing tables,
// so a partial drop leaves duplicate columns and a dirty migration version.
//
// 2026-11-18 数据事故护栏（规则见 excel-libs/testmysql）：本 helper 逐表 DROP，
// 执行前硬校验连接所在库必须以 _test 结尾（excel_core_test）；连到共享库时立即
// 失败，绝不落任何 DDL。
func dropAllTables(t *testing.T, db *sql.DB, label string) {
	t.Helper()
	testmysql.MustRequireTestDatabase(t, db, label)
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

func resetShareTestData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_share_links",
		"DELETE FROM excel_share_collaborators",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset share test data: %v", err)
		}
	}
	resetShareTestEvents(t, db)
}

func resetShareTestEvents(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(
		context.Background(),
		"DELETE FROM excel_bus_outbox WHERE base_id LIKE '01J00000000000000000003%'",
	); err != nil {
		t.Fatalf("reset share test events: %v", err)
	}
}
