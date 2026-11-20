// iam/invite_cross_enterprise_test.go：票 03（N7 收尾）邀请语义修复全链路用例。
// 注册自动建企（N7）后受邀用户必然已自有企业——邀请/接受不得再要求「用户无企业
// 归属」，接受 = 目标企业新增成员行（角色按邀请授予、不改写用户企业字段），企业内
// 鉴权按成员行判定（share CheckAccess 全链路验证，含外部协作者导出对照）。
package iam

import (
	"context"
	"database/sql"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel-libs/idgen"
	"excel-libs/testmysql"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
	"excel_core_service/internal/share"
	redisstore "excel_core_service/redis"
)

// inviteChainBaseResolver/Owner 把 excel_workspace_bases 直读为 share 的
// Base 寻址/所有者接缝（workspace 适配器反向依赖 iam，测试包内不能引入，
// 语义等价：同一张表、同样的 SQL 读取）。
type inviteChainBaseResolver struct {
	db *sql.DB
}

func (resolver inviteChainBaseResolver) EnterpriseID(
	ctx context.Context,
	baseID string,
) (string, error) {
	var enterpriseID string
	if err := resolver.db.QueryRowContext(ctx,
		"SELECT enterprise_id FROM excel_workspace_bases WHERE id = ?", baseID,
	).Scan(&enterpriseID); err != nil {
		return "", err
	}
	return enterpriseID, nil
}

func (resolver inviteChainBaseResolver) BaseName(
	ctx context.Context,
	baseID string,
) (string, error) {
	var name string
	if err := resolver.db.QueryRowContext(ctx,
		"SELECT name FROM excel_workspace_bases WHERE id = ?", baseID,
	).Scan(&name); err != nil {
		return "", err
	}
	return name, nil
}

func (resolver inviteChainBaseResolver) Importing(
	context.Context,
	string,
) (bool, error) {
	return false, nil
}

type inviteChainBaseOwner struct {
	db *sql.DB
}

func (provider inviteChainBaseOwner) OwnerID(
	ctx context.Context,
	baseID string,
) (string, error) {
	var ownerID string
	if err := provider.db.QueryRowContext(ctx,
		"SELECT owner_id FROM excel_workspace_bases WHERE id = ?", baseID,
	).Scan(&ownerID); err != nil {
		return "", err
	}
	return ownerID, nil
}

type inviteChainTestHarness struct {
	iamClient   iamv1.IamServiceClient
	shareClient sharev1.ShareServiceClient
	db          *sql.DB
}

func newInviteChainTestHarness(t *testing.T) *inviteChainTestHarness {
	t.Helper()

	db := testmysql.OpenTestDB(t, testMySQLDSN)
	resetCoreMigrationState(t, db)
	if err := base.MigrateUpWithRetry(db); err != nil {
		_ = db.Close()
		t.Fatalf("base.MigrateUpWithRetry() error = %v", err)
	}
	resetIAMTestData(t, db)

	redisClient := openIsolatedIAMTestRedisClient(t)

	iamRepository := base.NewIAMRepository(db)
	shareRepository := base.NewShareRepository(db)
	iamService := NewIamService(
		iamRepository,
		redisstore.NewSessionStore(redisClient),
		share.NewLinkResolver(shareRepository),
		bus.NewOutbox(base.NewBusStore(db)),
	)
	shareUserLookup := NewShareUserLookup(iamRepository)
	shareService, err := share.NewService(share.ServiceDeps{
		Repository:           shareRepository,
		Outbox:               bus.NewOutbox(base.NewBusStore(db)),
		Bases:                inviteChainBaseResolver{db: db},
		BaseNames:            inviteChainBaseResolver{db: db},
		BaseOwner:            inviteChainBaseOwner{db: db},
		ImportLock:           inviteChainBaseResolver{db: db},
		Members:              NewMemberRoleProvider(iamRepository),
		UserLookup:           shareUserLookup,
		Profiles:             shareUserLookup,
		EnterpriseDirectory:  NewEnterpriseDirectoryProvider(iamRepository),
		PermissionConfigPath: writeInviteChainPermissionPolicy(t),
	})
	if err != nil {
		t.Fatalf("share.NewService() error = %v", err)
	}

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	Register(server, iamService)
	share.Register(server, shareService)
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
		resetIAMTestData(t, db)
		if _, err := db.ExecContext(context.Background(),
			"DELETE FROM excel_workspace_bases"); err != nil {
			t.Fatalf("clean workspace bases: %v", err)
		}
		_ = redisClient.Close()
		_ = db.Close()
	})
	return &inviteChainTestHarness{
		iamClient:   iamv1.NewIamServiceClient(conn),
		shareClient: sharev1.NewShareServiceClient(conn),
		db:          db,
	}
}

// writeInviteChainPermissionPolicy 与 share 包测试同一份权限策略（editor 含 export）。
func writeInviteChainPermissionPolicy(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/permissions.json"
	payload := `{
		"schema_version": 1,
		"roles": {
			"manager": {"name": "可管理", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "manage_structure", "comment", "export", "manage_share", "copy_base", "import"]},
			"editor": {"name": "可编辑", "actions": ["view", "create_record", "edit_all", "delete_record", "manage_view", "comment", "export"]},
			"filler": {"name": "可填写", "actions": ["view_own", "create_record", "edit_own", "comment"]},
			"viewer": {"name": "可查看", "actions": ["view", "comment"]}
		},
		"external": {"export_allowed": false}
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write permission policy: %v", err)
	}
	return path
}

func (harness *inviteChainTestHarness) seedBase(
	t *testing.T,
	enterpriseID, ownerID, name string,
) string {
	t.Helper()
	baseID, err := idgen.New()
	if err != nil {
		t.Fatalf("generate base id: %v", err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := harness.db.ExecContext(context.Background(), `
		INSERT INTO excel_workspace_bases
			(id, enterprise_id, owner_id, name, last_modified_at, last_modifier, created_at)
		VALUES (?, ?, ?, ?, ?, 'test', ?)
	`, baseID, enterpriseID, ownerID, name, now, now); err != nil {
		t.Fatalf("seed base %q: %v", name, err)
	}
	return baseID
}

func (harness *inviteChainTestHarness) memberRowCount(
	t *testing.T,
	enterpriseID, userID string,
) int {
	t.Helper()
	var count int
	if err := harness.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM excel_iam_members WHERE enterprise_id = ? AND user_id = ?",
		enterpriseID,
		userID,
	).Scan(&count); err != nil {
		t.Fatalf("count member rows: %v", err)
	}
	return count
}

// 票 03 全链路：所有者邀请已自有企业的用户 → 接受 → 目标企业新增成员行（角色按
// 邀请授予）且用户自有企业与其下 Base 访问不受影响 → 以成员行归属访问目标企业
// Base（协作者编辑者 + 成员行 = 内部身份：导出放行；无成员行的外部协作者导出仍
// 拒绝）→ 重复接受幂等（不重复建行、不重复计数）。
func TestInviteMemberWithOwnEnterpriseJoinsTargetEnterprise(t *testing.T) {
	harness := newInviteChainTestHarness(t)

	owner := registerTestUser(t, harness.iamClient, "Chain_Owner", "目标企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	enterpriseID := owner.GetEnterpriseId()

	joiner := registerTestUser(t, harness.iamClient, "Chain_Joiner", "自有企业")
	joinerHome := joiner.GetEnterpriseId()
	joinerContext := contextWithPrincipal(t, testPrincipal(joiner))

	invitation := inviteTestUser(t, harness.iamClient, ownerContext, "Chain_Joiner")

	if _, err := harness.iamClient.AcceptInvite(joinerContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}

	var rowRole, rowStatus string
	if err := harness.db.QueryRowContext(context.Background(), `
		SELECT role, status FROM excel_iam_members
		WHERE enterprise_id = ? AND user_id = ?
	`, enterpriseID, joiner.GetId()).Scan(&rowRole, &rowStatus); err != nil {
		t.Fatalf("query joined member row: %v", err)
	}
	if rowRole != "member" || rowStatus != "active" {
		t.Fatalf("joined member row = role %q status %q, want member/active", rowRole, rowStatus)
	}
	if count := harness.memberRowCount(t, enterpriseID, joiner.GetId()); count != 1 {
		t.Fatalf("member rows for joiner in target enterprise = %d, want 1", count)
	}

	// 用户企业字段与自有企业访问不受影响：GetMe 仍展示自有企业（owner）。
	me, err := harness.iamClient.GetMe(joinerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMe(joiner) after accept error = %v", err)
	}
	if me.GetUser().GetEnterpriseId() != joinerHome {
		t.Fatalf("GetMe(joiner) user.enterprise_id = %q, want unchanged %q",
			me.GetUser().GetEnterpriseId(), joinerHome)
	}
	if me.GetEnterprise().GetId() != joinerHome || me.GetMyRole() != "owner" {
		t.Fatalf("GetMe(joiner) = enterprise %q role %q, want own enterprise owner",
			me.GetEnterprise().GetId(), me.GetMyRole())
	}
	target, err := harness.iamClient.GetEnterprise(ownerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterprise(target) error = %v", err)
	}
	if target.GetEnterprise().GetMemberCount() != 2 {
		t.Fatalf("target member_count = %d, want 2", target.GetEnterprise().GetMemberCount())
	}

	// 幂等一：同一邀请重复接受 → 成功空操作。
	if _, err := harness.iamClient.AcceptInvite(joinerContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("re-accept(same invitation) error = %v, want idempotent success", err)
	}

	// 已是本企业活跃成员的目标再次被邀请 → 明确拒绝（新文案），不重复建行、不计数。
	_, err = harness.iamClient.InviteMember(ownerContext, &iamv1.InviteMemberRequest{
		UsernameList: []string{"Chain_Joiner"},
	})
	if status.Code(err) != codes.AlreadyExists || errorReason(err) != itoa(deal.CodeConflict) {
		t.Fatalf("InviteMember(active member) error = %v, want 1002", err)
	}
	if message := status.Convert(err).Message(); message != "该用户已是本企业成员" {
		t.Fatalf("InviteMember(active member) message = %q, want 该用户已是本企业成员", message)
	}
	if count := harness.memberRowCount(t, enterpriseID, joiner.GetId()); count != 1 {
		t.Fatalf("member rows after duplicate accepts = %d, want 1", count)
	}
	target, err = harness.iamClient.GetEnterprise(ownerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterprise(target) after duplicate accepts error = %v", err)
	}
	if target.GetEnterprise().GetMemberCount() != 2 {
		t.Fatalf("target member_count after duplicate accepts = %d, want 2",
			target.GetEnterprise().GetMemberCount())
	}

	// 全链路鉴权：目标企业 Base + 协作者编辑者授予 → 受邀成员（成员行归属）可读
	// 可编辑且内部身份（导出放行）；无成员行的外部协作者同授权仍导出拒绝。
	baseID := harness.seedBase(t, enterpriseID, owner.GetId(), "目标企业 Base")
	if _, err := harness.shareClient.AddCollaborator(ownerContext, &sharev1.AddCollaboratorRequest{
		BaseId:   baseID,
		Username: "Chain_Joiner",
		Role:     sharev1.Role_ROLE_EDITOR,
	}); err != nil {
		t.Fatalf("AddCollaborator(joiner) error = %v", err)
	}
	external := registerTestUser(t, harness.iamClient, "Chain_External", "外部自有企业")
	if _, err := harness.shareClient.AddCollaborator(ownerContext, &sharev1.AddCollaboratorRequest{
		BaseId:   baseID,
		Username: "Chain_External",
		Role:     sharev1.Role_ROLE_EDITOR,
	}); err != nil {
		t.Fatalf("AddCollaborator(external) error = %v", err)
	}

	joinerAccess, err := harness.shareClient.CheckAccess(joinerContext, &sharev1.CheckAccessRequest{
		Principal: testPrincipal(joiner),
		BaseId:    baseID,
		Action:    sharev1.Action_ACTION_VIEW,
	})
	if err != nil {
		t.Fatalf("CheckAccess(joiner view) error = %v", err)
	}
	if !joinerAccess.GetAllow() || !joinerAccess.GetInternal() {
		t.Fatalf("CheckAccess(joiner view) = allow %v internal %v, want true/true",
			joinerAccess.GetAllow(), joinerAccess.GetInternal())
	}
	joinerExport, err := harness.shareClient.CheckAccess(joinerContext, &sharev1.CheckAccessRequest{
		Principal: testPrincipal(joiner),
		BaseId:    baseID,
		Action:    sharev1.Action_ACTION_EXPORT,
	})
	if err != nil {
		t.Fatalf("CheckAccess(joiner export) error = %v", err)
	}
	if !joinerExport.GetAllow() {
		t.Fatal("CheckAccess(joiner export) allow = false, want true (internal member)")
	}

	externalContext := contextWithPrincipal(t, testPrincipal(external))
	externalAccess, err := harness.shareClient.CheckAccess(externalContext, &sharev1.CheckAccessRequest{
		Principal: testPrincipal(external),
		BaseId:    baseID,
		Action:    sharev1.Action_ACTION_VIEW,
	})
	if err != nil {
		t.Fatalf("CheckAccess(external view) error = %v", err)
	}
	if !externalAccess.GetAllow() || externalAccess.GetInternal() {
		t.Fatalf("CheckAccess(external view) = allow %v internal %v, want true/false",
			externalAccess.GetAllow(), externalAccess.GetInternal())
	}
	externalExport, err := harness.shareClient.CheckAccess(externalContext, &sharev1.CheckAccessRequest{
		Principal: testPrincipal(external),
		BaseId:    baseID,
		Action:    sharev1.Action_ACTION_EXPORT,
	})
	if err != nil {
		t.Fatalf("CheckAccess(external export) error = %v", err)
	}
	if externalExport.GetAllow() {
		t.Fatal("CheckAccess(external export) allow = true, want false (external cap)")
	}

	// 自有企业 Base 访问不因加入他企业受损：受邀用户对自己企业的 Base 仍是所有者。
	joinerBaseID := harness.seedBase(t, joinerHome, joiner.GetId(), "自有企业 Base")
	ownAccess, err := harness.shareClient.CheckAccess(joinerContext, &sharev1.CheckAccessRequest{
		Principal: testPrincipal(joiner),
		BaseId:    joinerBaseID,
		Action:    sharev1.Action_ACTION_EXPORT,
	})
	if err != nil {
		t.Fatalf("CheckAccess(own base export) error = %v", err)
	}
	if !ownAccess.GetAllow() || !ownAccess.GetInternal() {
		t.Fatalf("CheckAccess(own base export) = allow %v internal %v, want true/true",
			ownAccess.GetAllow(), ownAccess.GetInternal())
	}
}
