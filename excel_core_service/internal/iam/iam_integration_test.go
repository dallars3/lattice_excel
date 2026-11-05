package iam

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	redisstore "excel_core_service/redis"
)

// Keep this destructive integration suite isolated from base's migration test.
// IAM 集成测试使用 core 库自身；跨包并发由 `go test -p 1` 串行化消除，
// 不再借用 excel_worker 库（否则会把 worker 迁移版本推进到 4，破坏 worker 测试）。
const testMySQLDSN = "excel:excel123@tcp(127.0.0.1:3306)/excel_core?charset=utf8mb4&parseTime=true"
const testRedisAddr = "127.0.0.1:6379"

type stubLinkResolver struct {
	linkContext *commonv1.LinkContext
	err         error
}

func (resolver stubLinkResolver) ResolveLinkToken(context.Context, string) (*commonv1.LinkContext, error) {
	return resolver.linkContext, resolver.err
}

type recordingLinkResolver struct {
	linkContext *commonv1.LinkContext
	token       string
}

func (resolver *recordingLinkResolver) ResolveLinkToken(_ context.Context, token string) (*commonv1.LinkContext, error) {
	resolver.token = token
	return resolver.linkContext, nil
}

func TestRegisterWithEnterpriseCreatesOwnerSession(t *testing.T) {
	client, db := newIAMTestClient(t)

	response, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username:       "Owner_01",
		Password:       "Pass1234",
		EnterpriseName: "启航科技",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if response.GetToken() == "" {
		t.Fatal("Register() token is empty")
	}
	if response.GetUser().GetEnterpriseId() == "" {
		t.Fatal("Register() enterprise_id is empty")
	}

	session, err := client.ExchangeSession(context.Background(), &iamv1.ExchangeSessionRequest{Token: response.GetToken()})
	if err != nil {
		t.Fatalf("ExchangeSession() error = %v", err)
	}
	principal := session.GetPrincipal()
	if principal.GetUserId() != response.GetUser().GetId() {
		t.Fatalf("principal.user_id = %q, want %q", principal.GetUserId(), response.GetUser().GetId())
	}
	if principal.GetEnterpriseId() != response.GetUser().GetEnterpriseId() {
		t.Fatalf("principal.enterprise_id = %q, want %q", principal.GetEnterpriseId(), response.GetUser().GetEnterpriseId())
	}
	if !principal.GetInternal() {
		t.Fatal("principal.internal = false, want true")
	}

	var role string
	if err := db.QueryRowContext(context.Background(),
		"SELECT role FROM excel_iam_members WHERE user_id = ?",
		response.GetUser().GetId(),
	).Scan(&role); err != nil {
		t.Fatalf("query registered member: %v", err)
	}
	if role != "owner" {
		t.Fatalf("member role = %q, want owner", role)
	}

	var passwordHash string
	if err := db.QueryRowContext(context.Background(),
		"SELECT password_hash FROM excel_iam_users WHERE id = ?",
		response.GetUser().GetId(),
	).Scan(&passwordHash); err != nil {
		t.Fatalf("query registered credential: %v", err)
	}
	if passwordHash == "Pass1234" || !strings.HasPrefix(passwordHash, "$2") {
		t.Fatalf("password_hash = %q, want bcrypt hash", passwordHash)
	}
}

func TestRegisterWithoutEnterpriseCreatesInternalAccount(t *testing.T) {
	client, _ := newIAMTestClient(t)

	response, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "Member_01",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if response.GetUser().GetEnterpriseId() != "" {
		t.Fatalf("Register() enterprise_id = %q, want empty", response.GetUser().GetEnterpriseId())
	}

	session, err := client.ExchangeSession(context.Background(), &iamv1.ExchangeSessionRequest{Token: response.GetToken()})
	if err != nil {
		t.Fatalf("ExchangeSession() error = %v", err)
	}
	if !session.GetPrincipal().GetInternal() {
		t.Fatal("principal.internal = false, want true")
	}
	if session.GetPrincipal().GetEnterpriseId() != "" {
		t.Fatalf("principal.enterprise_id = %q, want empty", session.GetPrincipal().GetEnterpriseId())
	}
	if session.GetPrincipal().GetLinkContext() != nil {
		t.Fatal("principal.link_context is set, want nil")
	}
}

func TestRegisterWithLinkCreatesExternalPrincipal(t *testing.T) {
	linkContext := &commonv1.LinkContext{
		BaseId:    "01J00000000000000000000001",
		LinkToken: "link_alice",
		Role:      "editor",
	}
	resolver := &recordingLinkResolver{linkContext: linkContext}
	client, _ := newIAMTestClientWithResolver(t, resolver)

	response, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username:  "Link_User",
		Password:  "Pass1234",
		LinkToken: "link_alice",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if resolver.token != "link_alice" {
		t.Fatalf("ResolveLinkToken() token = %q, want link_alice", resolver.token)
	}
	if response.GetUser().GetEnterpriseId() != "" {
		t.Fatalf("Register() enterprise_id = %q, want empty", response.GetUser().GetEnterpriseId())
	}

	session, err := client.ExchangeSession(context.Background(), &iamv1.ExchangeSessionRequest{Token: response.GetToken()})
	if err != nil {
		t.Fatalf("ExchangeSession() error = %v", err)
	}
	principal := session.GetPrincipal()
	if principal.GetInternal() {
		t.Fatal("principal.internal = true, want false")
	}
	if principal.GetLinkContext().GetBaseId() != linkContext.BaseId ||
		principal.GetLinkContext().GetLinkToken() != linkContext.LinkToken ||
		principal.GetLinkContext().GetRole() != linkContext.Role {
		t.Fatalf("principal.link_context = %v, want %v", principal.GetLinkContext(), linkContext)
	}
}

func TestLoginLocksUsernameAndIPAfterFiveFailures(t *testing.T) {
	client, _ := newIAMTestClient(t)
	loginContext := metadata.AppendToOutgoingContext(context.Background(), "x-client-ip", "192.0.2.10")

	for _, username := range []string{"Lock_User", "Other_User"} {
		if _, err := client.Register(context.Background(), &iamv1.RegisterRequest{
			Username: username,
			Password: "Pass1234",
		}); err != nil {
			t.Fatalf("Register(%q) error = %v", username, err)
		}
	}

	for attempt := 1; attempt <= 5; attempt++ {
		_, err := client.Login(loginContext, &iamv1.LoginRequest{
			Username: "Lock_User",
			Password: "Wrong1234",
		})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("Login() attempt %d error = %v, want Unauthenticated", attempt, err)
		}
	}

	_, err := client.Login(loginContext, &iamv1.LoginRequest{
		Username: "Lock_User",
		Password: "Pass1234",
	})
	if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "登录失败或已锁定" {
		t.Fatalf("locked Login() error = %v, want 2002 login failure", err)
	}

	redisClient := goredis.NewClient(&goredis.Options{Addr: testRedisAddr})
	defer redisClient.Close()
	lockKeys, err := redisClient.Keys(context.Background(), "excel:iam:login:lock:*").Result()
	if err != nil {
		t.Fatalf("list login lock keys: %v", err)
	}
	if len(lockKeys) != 1 {
		t.Fatalf("login lock keys = %v, want one username+IP lock", lockKeys)
	}
	ttl, err := redisClient.TTL(context.Background(), lockKeys[0]).Result()
	if err != nil {
		t.Fatalf("read login lock TTL: %v", err)
	}
	if ttl < 15*time.Minute-10*time.Second || ttl > 15*time.Minute {
		t.Fatalf("login lock TTL = %v, want 15 minutes", ttl)
	}

	if _, err := client.Login(loginContext, &iamv1.LoginRequest{
		Username: "Other_User",
		Password: "Pass1234",
	}); err != nil {
		t.Fatalf("unrelated user Login() error = %v", err)
	}

	otherIP := metadata.AppendToOutgoingContext(context.Background(), "x-client-ip", "192.0.2.11")
	if _, err := client.Login(otherIP, &iamv1.LoginRequest{
		Username: "Lock_User",
		Password: "Pass1234",
	}); err != nil {
		t.Fatalf("same username from another IP Login() error = %v", err)
	}
}

func TestLogoutInvalidatesOnlyCurrentSession(t *testing.T) {
	client, _ := newIAMTestClient(t)
	registration, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "Logout_User",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	first, err := client.Login(context.Background(), &iamv1.LoginRequest{
		Username: "Logout_User",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("first Login() error = %v", err)
	}
	second, err := client.Login(context.Background(), &iamv1.LoginRequest{
		Username: "Logout_User",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("second Login() error = %v", err)
	}

	if _, err := client.Logout(context.Background(), &iamv1.LogoutRequest{Token: first.GetToken()}); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := client.ExchangeSession(
		context.Background(),
		&iamv1.ExchangeSessionRequest{Token: first.GetToken()},
	); status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "未登录" {
		t.Fatalf("ExchangeSession(revoked token) error = %v, want 2001", err)
	}
	if _, err := client.ExchangeSession(
		context.Background(),
		&iamv1.ExchangeSessionRequest{Token: second.GetToken()},
	); err != nil {
		t.Fatalf("ExchangeSession(other session) error = %v", err)
	}

	if _, err := client.ExchangeSession(
		context.Background(),
		&iamv1.ExchangeSessionRequest{Token: registration.GetToken()},
	); err != nil {
		t.Fatalf("ExchangeSession(registration session) error = %v", err)
	}
}

func TestGetMeUsesPrincipalAndReturnsOwnerContext(t *testing.T) {
	client, _ := newIAMTestClient(t)
	registration, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username:       "Me_Owner",
		Password:       "Pass1234",
		EnterpriseName: "远航科技",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	session, err := client.ExchangeSession(
		context.Background(),
		&iamv1.ExchangeSessionRequest{Token: registration.GetToken()},
	)
	if err != nil {
		t.Fatalf("ExchangeSession() error = %v", err)
	}

	response, err := client.GetMe(contextWithPrincipal(t, session.GetPrincipal()), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMe() error = %v", err)
	}
	if response.GetUser().GetUsername() != "Me_Owner" {
		t.Fatalf("GetMe() username = %q, want Me_Owner", response.GetUser().GetUsername())
	}
	if response.GetEnterprise().GetName() != "远航科技" {
		t.Fatalf("GetMe() enterprise = %q, want 远航科技", response.GetEnterprise().GetName())
	}
	if response.GetMyRole() != "owner" {
		t.Fatalf("GetMe() my_role = %q, want owner", response.GetMyRole())
	}

	if _, err := client.GetMe(context.Background(), &emptypb.Empty{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("GetMe(no principal) error = %v, want Unauthenticated", err)
	}
}

func TestUpdateProfileChangesAuthenticatedUser(t *testing.T) {
	client, _ := newIAMTestClient(t)
	registration, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "Profile_User",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	session, err := client.ExchangeSession(
		context.Background(),
		&iamv1.ExchangeSessionRequest{Token: registration.GetToken()},
	)
	if err != nil {
		t.Fatalf("ExchangeSession() error = %v", err)
	}
	authenticated := contextWithPrincipal(t, session.GetPrincipal())

	response, err := client.UpdateProfile(authenticated, &iamv1.UpdateProfileRequest{
		DisplayName: "陈晨",
		AvatarUri:   "/avatars/chen.png",
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if response.GetUser().GetDisplayName() != "陈晨" || response.GetUser().GetAvatarUri() != "/avatars/chen.png" {
		t.Fatalf("UpdateProfile() user = %v", response.GetUser())
	}

	me, err := client.GetMe(authenticated, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMe() after profile update error = %v", err)
	}
	if me.GetUser().GetDisplayName() != "陈晨" {
		t.Fatalf("GetMe() display_name = %q, want 陈晨", me.GetUser().GetDisplayName())
	}

	if _, err := client.UpdateProfile(authenticated, &iamv1.UpdateProfileRequest{
		DisplayName: "陈晨",
		AvatarUri:   "/avatars/chen.png",
	}); err != nil {
		t.Fatalf("UpdateProfile(unchanged values) error = %v", err)
	}
}

func TestChangePasswordRequiresOldPasswordAndNewPasswordPolicy(t *testing.T) {
	client, _ := newIAMTestClient(t)
	registration, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "Password_User",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	session, err := client.ExchangeSession(
		context.Background(),
		&iamv1.ExchangeSessionRequest{Token: registration.GetToken()},
	)
	if err != nil {
		t.Fatalf("ExchangeSession() error = %v", err)
	}
	authenticated := contextWithPrincipal(t, session.GetPrincipal())

	_, err = client.ChangePassword(authenticated, &iamv1.ChangePasswordRequest{
		OldPassword: "Wrong1234",
		NewPassword: "NewPass5678",
	})
	if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "登录失败或已锁定" {
		t.Fatalf("ChangePassword(old mismatch) error = %v, want 2002", err)
	}

	_, err = client.ChangePassword(authenticated, &iamv1.ChangePasswordRequest{
		OldPassword: "Pass1234",
		NewPassword: "NoDigits",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ChangePassword(invalid new) error = %v, want 1001", err)
	}

	if _, err := client.ChangePassword(authenticated, &iamv1.ChangePasswordRequest{
		OldPassword: "Pass1234",
		NewPassword: "NewPass5678",
	}); err != nil {
		t.Fatalf("ChangePassword(success) error = %v", err)
	}
	if _, err := client.Login(context.Background(), &iamv1.LoginRequest{
		Username: "Password_User",
		Password: "NewPass5678",
	}); err != nil {
		t.Fatalf("Login(new password) error = %v", err)
	}
}

func TestRegisterRejectsCaseInsensitiveDuplicateAndInvalidCredentials(t *testing.T) {
	client, _ := newIAMTestClient(t)

	invalidRequests := []*iamv1.RegisterRequest{
		{Username: "1bad", Password: "Pass1234"},
		{Username: "Valid_User", Password: "abcdefgh"},
	}
	for _, request := range invalidRequests {
		if _, err := client.Register(context.Background(), request); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Register(%+v) error = %v, want 1001", request, err)
		}
	}

	if _, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "Case_User",
		Password: "Pass1234",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "case_user",
		Password: "Pass1234",
	}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("Register(case-insensitive duplicate) error = %v, want 1002", err)
	}
}

func TestSuccessfulLoginResetsFailuresAndIssuesThirtyDayTicket(t *testing.T) {
	client, _ := newIAMTestClient(t)
	if _, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "Ticket_User",
		Password: "Pass1234",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	loginContext := metadata.AppendToOutgoingContext(context.Background(), "x-client-ip", "198.51.100.20")

	var loginResponse *iamv1.LoginResponse
	for cycle := 1; cycle <= 2; cycle++ {
		for attempt := 1; attempt <= 4; attempt++ {
			_, err := client.Login(loginContext, &iamv1.LoginRequest{
				Username: "Ticket_User",
				Password: "Wrong1234",
			})
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("cycle %d failure %d Login() error = %v", cycle, attempt, err)
			}
		}
		response, err := client.Login(loginContext, &iamv1.LoginRequest{
			Username: "Ticket_User",
			Password: "Pass1234",
		})
		if err != nil {
			t.Fatalf("cycle %d successful Login() error = %v", cycle, err)
		}
		loginResponse = response
	}

	redisClient := goredis.NewClient(&goredis.Options{Addr: testRedisAddr})
	defer redisClient.Close()
	ttl, err := redisClient.TTL(
		context.Background(),
		"excel:iam:session:"+loginResponse.GetToken(),
	).Result()
	if err != nil {
		t.Fatalf("read session TTL: %v", err)
	}
	if ttl < 30*24*time.Hour-10*time.Second || ttl > 30*24*time.Hour {
		t.Fatalf("session TTL = %v, want 30 days", ttl)
	}
}

func contextWithPrincipal(t *testing.T, principal *commonv1.Principal) context.Context {
	t.Helper()
	payload, err := proto.Marshal(principal)
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
}

func newIAMTestClient(t *testing.T) (iamv1.IamServiceClient, *sql.DB) {
	return newIAMTestClientWithResolver(t, stubLinkResolver{})
}

func newIAMTestClientWithResolver(t *testing.T, resolver LinkResolver) (iamv1.IamServiceClient, *sql.DB) {
	t.Helper()

	db, err := base.OpenDB(testMySQLDSN)
	if err != nil {
		t.Fatalf("base.OpenDB() error = %v", err)
	}
	resetCoreMigrationState(t, db)
	if err := base.MigrateUp(db); err != nil {
		_ = db.Close()
		t.Fatalf("base.MigrateUp() error = %v", err)
	}
	resetIAMTestData(t, db)

	redisClient := goredis.NewClient(&goredis.Options{Addr: testRedisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		_ = db.Close()
		_ = redisClient.Close()
		t.Fatalf("redis ping: %v", err)
	}
	_ = redisClient.FlushDB(context.Background()).Err()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	service := NewIamService(
		base.NewIAMRepository(db),
		redisstore.NewSessionStore(redisClient),
		resolver,
		bus.NewOutbox(base.NewBusStore(db)),
	)
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
		resetIAMTestData(t, db)
		_ = redisClient.Close()
		_ = db.Close()
	})
	return iamv1.NewIamServiceClient(conn), db
}

func resetCoreMigrationState(t *testing.T, db *sql.DB) {
	t.Helper()
	if !coreMigrationIsDirty(db) {
		return
	}
	dropAllTables(t, db, "reset core migration state")
}

// coreMigrationIsDirty reports whether schema_migrations holds a dirty version
// (a half-applied migration that golang-migrate refuses to advance past). A
// clean (or missing) version table can be brought up to date by MigrateUp
// directly, so the expensive drop-all-and-replay path is only taken on recovery.
func coreMigrationIsDirty(db *sql.DB) bool {
	var dirty bool
	err := db.QueryRowContext(context.Background(),
		"SELECT dirty FROM schema_migrations WHERE dirty = 1 LIMIT 1").Scan(&dirty)
	return err == nil && dirty
}

// dropAllTables removes every table in the current database so migrations can
// replay from scratch. Migrations 000015/000016/000018 ALTER pre-existing tables,
// so a partial drop leaves duplicate columns and a dirty migration version.
func dropAllTables(t *testing.T, db *sql.DB, label string) {
	t.Helper()
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

func resetIAMTestData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DELETE FROM excel_bus_outbox",
		"DELETE FROM excel_iam_invitations",
		"DELETE FROM excel_iam_members",
		"DELETE FROM excel_iam_enterprises",
		"DELETE FROM excel_iam_users",
		"UPDATE excel_iam_register_switch SET enabled = 1 WHERE id = 1",
	} {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("reset IAM test data: %v", err)
		}
	}
}
