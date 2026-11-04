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
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
)

const (
	shareTestMySQLDSN  = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"
	shareTestRedisAddr = "127.0.0.1:6379"
)

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

func (provider shareMemberRoleProvider) Role(_ context.Context, enterpriseID, userID string) (iamv1.MemberRole, error) {
	role, ok := provider[enterpriseID+"/"+userID]
	if !ok {
		return iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, sql.ErrNoRows
	}
	return role, nil
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

type shareTestHarness struct {
	client           sharev1.ShareServiceClient
	db               *sql.DB
	baseResolver     shareBaseResolver
	baseOwner        shareBaseOwnerProvider
	importLock       shareImportLock
	memberRole       shareMemberRoleProvider
	userLookup       shareUserLookup
	permissionConfig string
	events           *eventbus.MemoryBus
	forwarder        *bus.Forwarder
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

	db, err := base.OpenDB(shareTestMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
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
	baseOwner := shareBaseOwnerProvider{}
	importLock := shareImportLock{}
	memberRoles := shareMemberRoleProvider{}
	userLookup := shareUserLookup{}
	events := eventbus.NewMemoryBus()
	outbox := bus.NewOutbox(base.NewBusStore(db))
	service, err := NewService(ServiceDeps{
		Repository:           base.NewShareRepository(db),
		Bases:                baseResolver,
		BaseOwner:            baseOwner,
		ImportLock:           importLock,
		Members:              memberRoles,
		UserLookup:           userLookup,
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
		client:           sharev1.NewShareServiceClient(conn),
		db:               db,
		baseResolver:     baseResolver,
		baseOwner:        baseOwner,
		importLock:       importLock,
		memberRole:       memberRoles,
		userLookup:       userLookup,
		permissionConfig: permissionConfig,
		events:           events,
		forwarder:        bus.NewForwarder(base.NewBusStore(db), events, bus.ForwarderConfig{}),
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
	for _, statement := range []string{
		"DROP TABLE IF EXISTS excel_share_links",
		"DROP TABLE IF EXISTS excel_share_collaborators",
		// 迁移重放要求无残留 schema：000015 起存在对既有表的 ALTER，
		// workspace 三表必须随 schema_migrations 一并重建。
		"DROP TABLE IF EXISTS excel_workspace_bases",
		"DROP TABLE IF EXISTS excel_workspace_stars",
		"DROP TABLE IF EXISTS excel_workspace_recent",
		"DROP TABLE IF EXISTS schema_migrations",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset share migration state: %v", err)
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
