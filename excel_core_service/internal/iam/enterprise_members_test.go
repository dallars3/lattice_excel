package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"strconv"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/deal"
	redisstore "excel_core_service/redis"
)

type enterpriseTestHarness struct {
	client    iamv1.IamServiceClient
	db        *sql.DB
	events    *eventbus.MemoryBus
	forwarder *bus.Forwarder
	bases     *recordingBaseReassigner
}

func newEnterpriseTestHarness(t *testing.T) *enterpriseTestHarness {
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

	events := eventbus.NewMemoryBus()
	bases := &recordingBaseReassigner{}
	service := NewIamService(
		base.NewIAMRepository(db),
		redisstore.NewSessionStore(redisClient),
		stubLinkResolver{},
		bus.NewOutbox(base.NewBusStore(db)),
		bases,
	)

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
		resetIAMTestData(t, db)
		_ = redisClient.Close()
		_ = db.Close()
	})
	return &enterpriseTestHarness{
		client:    iamv1.NewIamServiceClient(conn),
		db:        db,
		events:    events,
		forwarder: bus.NewForwarder(base.NewBusStore(db), events, bus.ForwarderConfig{}),
		bases:     bases,
	}
}

type recordingBaseReassigner struct {
	fromUserIDs []string
	toUserIDs   []string
	err         error
}

func (reassigner *recordingBaseReassigner) ReassignAllBases(
	_ context.Context,
	fromUserID, toUserID string,
) error {
	reassigner.fromUserIDs = append(reassigner.fromUserIDs, fromUserID)
	reassigner.toUserIDs = append(reassigner.toUserIDs, toUserID)
	return reassigner.err
}

func TestInviteUnregisteredThenRegisterAndAcceptJoinsEnterprise(t *testing.T) {
	harness := newEnterpriseTestHarness(t)

	owner := registerTestUser(t, harness.client, "Owner_Flow", "企业甲")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	inviteResponse, err := harness.client.InviteMember(ownerContext, &iamv1.InviteMemberRequest{
		UsernameList: []string{"Joiner_Flow"},
	})
	if err != nil {
		t.Fatalf("InviteMember() error = %v", err)
	}
	if len(inviteResponse.GetInvitationList()) != 1 {
		t.Fatalf("InviteMember() invitation count = %d, want 1", len(inviteResponse.GetInvitationList()))
	}
	invitation := inviteResponse.GetInvitationList()[0]
	if invitation.GetUsername() != "Joiner_Flow" ||
		invitation.GetEnterpriseId() != owner.GetEnterpriseId() ||
		invitation.GetEnterpriseName() != "企业甲" ||
		invitation.GetStatus() != iamv1.InvitationStatus_INVITATION_STATUS_PENDING {
		t.Fatalf("InviteMember() invitation = %v", invitation)
	}
	var invitedUserID sql.NullString
	if err := harness.db.QueryRowContext(context.Background(),
		"SELECT user_id FROM excel_iam_invitations WHERE id = ?",
		invitation.GetId(),
	).Scan(&invitedUserID); err != nil {
		t.Fatalf("query unregistered invite: %v", err)
	}
	if invitedUserID.Valid {
		t.Fatalf("unregistered invite user_id = %q, want NULL", invitedUserID.String)
	}

	joiner := registerTestUser(t, harness.client, "Joiner_Flow", "")
	joinerContext := contextWithPrincipal(t, testPrincipal(joiner))
	pending, err := harness.client.ListMyInvitations(joinerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListMyInvitations() error = %v", err)
	}
	if len(pending.GetInvitationList()) != 1 ||
		pending.GetInvitationList()[0].GetId() != invitation.GetId() {
		t.Fatalf("ListMyInvitations() = %v, want invite %s", pending.GetInvitationList(), invitation.GetId())
	}

	if _, err := harness.client.AcceptInvite(joinerContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}
	me, err := harness.client.GetMe(joinerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMe() after accept error = %v", err)
	}
	if me.GetEnterprise().GetId() != owner.GetEnterpriseId() || me.GetMyRole() != "member" {
		t.Fatalf("accepted membership = enterprise %q role %q", me.GetEnterprise().GetId(), me.GetMyRole())
	}

	_, err = harness.client.InviteMember(ownerContext, &iamv1.InviteMemberRequest{
		UsernameList: []string{"Joiner_Flow"},
	})
	if status.Code(err) != codes.AlreadyExists || errorReason(err) != itoa(deal.CodeConflict) {
		t.Fatalf("InviteMember(existing member) error = %v, want 1002", err)
	}
}

func TestDisableMemberRevokesSessionReassignsBasesAndEnableRestores(t *testing.T) {
	harness := newEnterpriseTestHarness(t)

	owner := registerTestUser(t, harness.client, "Owner_Disable", "停用演练企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	joiner := registerTestUser(t, harness.client, "Joiner_Disable", "")
	joinerContext := contextWithPrincipal(t, testPrincipal(joiner))
	invitation := inviteTestUser(t, harness.client, ownerContext, "Joiner_Disable")
	if _, err := harness.client.AcceptInvite(joinerContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}

	login, err := harness.client.Login(context.Background(), &iamv1.LoginRequest{
		Username: "Joiner_Disable",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if _, err := harness.client.ExchangeSession(context.Background(), &iamv1.ExchangeSessionRequest{
		Token: login.GetToken(),
	}); err != nil {
		t.Fatalf("ExchangeSession(active member) error = %v", err)
	}

	if _, err := harness.client.DisableMember(ownerContext, &iamv1.DisableMemberRequest{
		UserId: joiner.GetId(),
	}); err != nil {
		t.Fatalf("DisableMember() error = %v", err)
	}
	if len(harness.bases.fromUserIDs) != 1 ||
		harness.bases.fromUserIDs[0] != joiner.GetId() ||
		harness.bases.toUserIDs[0] != owner.GetId() {
		t.Fatalf("BaseReassigner calls = from %v to %v, want %s -> %s",
			harness.bases.fromUserIDs,
			harness.bases.toUserIDs,
			joiner.GetId(),
			owner.GetId(),
		)
	}
	_, err = harness.client.ExchangeSession(context.Background(), &iamv1.ExchangeSessionRequest{
		Token: login.GetToken(),
	})
	if status.Code(err) != codes.Unauthenticated || errorReason(err) != itoa(deal.CodeUnauthenticated) {
		t.Fatalf("ExchangeSession(disabled member) error = %v, want 2001", err)
	}
	var statusValue, enterpriseID string
	if err := harness.db.QueryRowContext(context.Background(),
		`SELECT m.status, u.enterprise_id
		 FROM excel_iam_members m
		 JOIN excel_iam_users u ON u.id = m.user_id
		 WHERE m.user_id = ?`,
		joiner.GetId(),
	).Scan(&statusValue, &enterpriseID); err != nil {
		t.Fatalf("query disabled member: %v", err)
	}
	if statusValue != "disabled" || enterpriseID != owner.GetEnterpriseId() {
		t.Fatalf("disabled member status = %q enterprise = %q", statusValue, enterpriseID)
	}

	if _, err := harness.client.EnableMember(ownerContext, &iamv1.EnableMemberRequest{
		UserId: joiner.GetId(),
	}); err != nil {
		t.Fatalf("EnableMember() error = %v", err)
	}
	restored, err := harness.client.Login(context.Background(), &iamv1.LoginRequest{
		Username: "Joiner_Disable",
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Login() after enable error = %v", err)
	}
	session, err := harness.client.ExchangeSession(context.Background(), &iamv1.ExchangeSessionRequest{
		Token: restored.GetToken(),
	})
	if err != nil {
		t.Fatalf("ExchangeSession(enabled member) error = %v", err)
	}
	if session.GetPrincipal().GetEnterpriseId() != owner.GetEnterpriseId() {
		t.Fatalf("enabled member enterprise = %q, want %q",
			session.GetPrincipal().GetEnterpriseId(),
			owner.GetEnterpriseId(),
		)
	}
}

func TestMemberRoleAndOwnerTransferGuards(t *testing.T) {
	harness := newEnterpriseTestHarness(t)

	owner := registerTestUser(t, harness.client, "Owner_Roles", "角色守卫企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	joiner := registerTestUser(t, harness.client, "Joiner_Roles", "")
	joinerContext := contextWithPrincipal(t, testPrincipal(joiner))
	invitation := inviteTestUser(t, harness.client, ownerContext, "Joiner_Roles")
	if _, err := harness.client.AcceptInvite(joinerContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}

	_, err := harness.client.UpdateMemberRole(ownerContext, &iamv1.UpdateMemberRoleRequest{
		UserId: owner.GetId(),
		Role:   iamv1.MemberRole_MEMBER_ROLE_MANAGER,
	})
	if status.Code(err) != codes.PermissionDenied || errorReason(err) != itoa(deal.CodePermissionDenied) {
		t.Fatalf("UpdateMemberRole(owner self downgrade) error = %v, want 3001", err)
	}
	_, err = harness.client.UpdateMemberRole(ownerContext, &iamv1.UpdateMemberRoleRequest{
		UserId: joiner.GetId(),
		Role:   iamv1.MemberRole_MEMBER_ROLE_OWNER,
	})
	if status.Code(err) != codes.InvalidArgument || errorReason(err) != itoa(deal.CodeInvalidArgument) {
		t.Fatalf("UpdateMemberRole(direct owner assignment) error = %v, want 1001", err)
	}
	if _, err := harness.client.UpdateMemberRole(ownerContext, &iamv1.UpdateMemberRoleRequest{
		UserId: joiner.GetId(),
		Role:   iamv1.MemberRole_MEMBER_ROLE_MANAGER,
	}); err != nil {
		t.Fatalf("UpdateMemberRole(promote manager) error = %v", err)
	}
	_, err = harness.client.UpdateMemberRole(joinerContext, &iamv1.UpdateMemberRoleRequest{
		UserId: joiner.GetId(),
		Role:   iamv1.MemberRole_MEMBER_ROLE_MEMBER,
	})
	if status.Code(err) != codes.PermissionDenied || errorReason(err) != itoa(deal.CodePermissionDenied) {
		t.Fatalf("UpdateMemberRole(manager self downgrade) error = %v, want 3001", err)
	}
	_, err = harness.client.TransferOwner(ownerContext, &iamv1.TransferOwnerRequest{
		SuccessorId: owner.GetId(),
		Confirm:     true,
	})
	if status.Code(err) != codes.PermissionDenied || errorReason(err) != itoa(deal.CodePermissionDenied) {
		t.Fatalf("TransferOwner(self) error = %v, want 3001", err)
	}
	_, err = harness.client.TransferOwner(ownerContext, &iamv1.TransferOwnerRequest{
		Confirm: true,
	})
	if status.Code(err) != codes.InvalidArgument || errorReason(err) != itoa(deal.CodeInvalidArgument) {
		t.Fatalf("TransferOwner(no successor) error = %v, want 1001", err)
	}

	if _, err := harness.client.TransferOwner(ownerContext, &iamv1.TransferOwnerRequest{
		SuccessorId: joiner.GetId(),
		Confirm:     true,
	}); err != nil {
		t.Fatalf("TransferOwner(successor) error = %v", err)
	}
	var ownerID, oldRole, newRole string
	if err := harness.db.QueryRowContext(context.Background(),
		`SELECT e.owner_id,
		        (SELECT role FROM excel_iam_members WHERE enterprise_id = e.id AND user_id = ?),
		        (SELECT role FROM excel_iam_members WHERE enterprise_id = e.id AND user_id = ?)
		 FROM excel_iam_enterprises e WHERE e.id = ?`,
		owner.GetId(),
		joiner.GetId(),
		owner.GetEnterpriseId(),
	).Scan(&ownerID, &oldRole, &newRole); err != nil {
		t.Fatalf("query transferred owner roles: %v", err)
	}
	if ownerID != joiner.GetId() || oldRole != "manager" || newRole != "owner" {
		t.Fatalf("transferred owner = %q old role = %q new role = %q", ownerID, oldRole, newRole)
	}
}

func TestEnterpriseMemberEventsAreObservableOnMemoryBus(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	topics := []string{
		"invite.created",
		"invite.canceled",
		"member.disabled",
		"member.enabled",
		"enterprise.owner_transferred",
	}
	subscriptions := make(map[string]eventbus.Subscription, len(topics))
	for _, topic := range topics {
		subscription, err := harness.events.Subscribe(
			context.Background(),
			eventbus.SubscribeOptions{Topic: topic},
		)
		if err != nil {
			t.Fatalf("Subscribe(%q) error = %v", topic, err)
		}
		t.Cleanup(func() { _ = subscription.Close() })
		subscriptions[topic] = subscription
	}

	owner := registerTestUser(t, harness.client, "Owner_Events", "事件观察企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	joiner := registerTestUser(t, harness.client, "Joiner_Events", "")
	joinerContext := contextWithPrincipal(t, testPrincipal(joiner))

	firstInvite := inviteTestUser(t, harness.client, ownerContext, "Joiner_Events")
	created := nextTestEvent(t, harness, subscriptions["invite.created"])
	assertEventPayload(t, created, map[string]string{
		"enterprise_id": owner.GetEnterpriseId(),
		"username":      "Joiner_Events",
		"invite_id":     firstInvite.GetId(),
	})

	if _, err := harness.client.CancelInvite(ownerContext, &iamv1.CancelInviteRequest{
		InviteId: firstInvite.GetId(),
	}); err != nil {
		t.Fatalf("CancelInvite() error = %v", err)
	}
	canceled := nextTestEvent(t, harness, subscriptions["invite.canceled"])
	assertEventPayload(t, canceled, map[string]string{
		"enterprise_id": owner.GetEnterpriseId(),
		"username":      "Joiner_Events",
		"invite_id":     firstInvite.GetId(),
	})

	secondInvite := inviteTestUser(t, harness.client, ownerContext, "Joiner_Events")
	_ = nextTestEvent(t, harness, subscriptions["invite.created"])
	if _, err := harness.client.AcceptInvite(joinerContext, &iamv1.AcceptInviteRequest{
		InviteId: secondInvite.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}

	if _, err := harness.client.DisableMember(ownerContext, &iamv1.DisableMemberRequest{
		UserId: joiner.GetId(),
	}); err != nil {
		t.Fatalf("DisableMember() error = %v", err)
	}
	disabled := nextTestEvent(t, harness, subscriptions["member.disabled"])
	assertEventPayload(t, disabled, map[string]string{
		"enterprise_id": owner.GetEnterpriseId(),
		"user_id":       joiner.GetId(),
		"operator":      owner.GetId(),
	})

	if _, err := harness.client.EnableMember(ownerContext, &iamv1.EnableMemberRequest{
		UserId: joiner.GetId(),
	}); err != nil {
		t.Fatalf("EnableMember() error = %v", err)
	}
	enabled := nextTestEvent(t, harness, subscriptions["member.enabled"])
	assertEventPayload(t, enabled, map[string]string{
		"enterprise_id": owner.GetEnterpriseId(),
		"user_id":       joiner.GetId(),
		"operator":      owner.GetId(),
	})

	if _, err := harness.client.TransferOwner(ownerContext, &iamv1.TransferOwnerRequest{
		SuccessorId: joiner.GetId(),
		Confirm:     true,
	}); err != nil {
		t.Fatalf("TransferOwner() error = %v", err)
	}
	transferred := nextTestEvent(t, harness, subscriptions["enterprise.owner_transferred"])
	assertEventPayload(t, transferred, map[string]string{
		"enterprise_id": owner.GetEnterpriseId(),
		"from_user_id":  owner.GetId(),
		"to_user_id":    joiner.GetId(),
	})
}

func TestEnterpriseQueriesPaginationRejectAndRoleProvider(t *testing.T) {
	harness := newEnterpriseTestHarness(t)

	owner := registerTestUser(t, harness.client, "Owner_Query", "查询分页企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	memberUser := registerTestUser(t, harness.client, "Member_Query", "")
	memberContext := contextWithPrincipal(t, testPrincipal(memberUser))
	invite := inviteTestUser(t, harness.client, ownerContext, "Member_Query")
	if _, err := harness.client.AcceptInvite(memberContext, &iamv1.AcceptInviteRequest{
		InviteId: invite.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}

	enterprise, err := harness.client.GetEnterprise(ownerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterprise() error = %v", err)
	}
	if enterprise.GetEnterprise().GetName() != "查询分页企业" ||
		enterprise.GetEnterprise().GetMemberCount() != 2 {
		t.Fatalf("GetEnterprise() = %v", enterprise.GetEnterprise())
	}

	outsider := registerTestUser(t, harness.client, "Outsider_Query", "")
	outsiderContext := contextWithPrincipal(t, testPrincipal(outsider))
	pendingInvite := inviteTestUser(t, harness.client, ownerContext, "Outsider_Query")
	invites, err := harness.client.ListInvites(ownerContext, &iamv1.ListInvitesRequest{Limit: 1})
	if err != nil {
		t.Fatalf("ListInvites() error = %v", err)
	}
	if len(invites.GetInvitationList()) != 1 || invites.GetInvitationList()[0].GetId() != pendingInvite.GetId() {
		t.Fatalf("ListInvites() = %v", invites.GetInvitationList())
	}
	members, err := harness.client.ListMembers(ownerContext, &iamv1.ListMembersRequest{
		Q:     "query",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListMembers() error = %v", err)
	}
	if len(members.GetMemberList()) != 1 || !members.GetHasMore() || members.GetNextCursor() == "" {
		t.Fatalf("ListMembers(limit 1) = %+v", members)
	}

	provider := NewMemberRoleProvider(base.NewIAMRepository(harness.db))
	ownerRole, err := provider.Role(context.Background(), owner.GetEnterpriseId(), owner.GetId())
	if err != nil || ownerRole != iamv1.MemberRole_MEMBER_ROLE_OWNER {
		t.Fatalf("MemberRoleProvider.Role(owner) = %v, %v", ownerRole, err)
	}
	memberRole, err := provider.Role(context.Background(), owner.GetEnterpriseId(), memberUser.GetId())
	if err != nil || memberRole != iamv1.MemberRole_MEMBER_ROLE_MEMBER {
		t.Fatalf("MemberRoleProvider.Role(member) = %v, %v", memberRole, err)
	}

	if _, err := harness.client.RejectInvite(outsiderContext, &iamv1.RejectInviteRequest{
		InviteId: pendingInvite.GetId(),
	}); err != nil {
		t.Fatalf("RejectInvite() error = %v", err)
	}
	remaining, err := harness.client.ListMyInvitations(outsiderContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListMyInvitations() after reject error = %v", err)
	}
	if len(remaining.GetInvitationList()) != 0 {
		t.Fatalf("ListMyInvitations() after reject = %v", remaining.GetInvitationList())
	}
	if _, err := harness.client.DisableMember(ownerContext, &iamv1.DisableMemberRequest{
		UserId: memberUser.GetId(),
	}); err != nil {
		t.Fatalf("DisableMember() error = %v", err)
	}
	disabledRole, err := provider.Role(context.Background(), owner.GetEnterpriseId(), memberUser.GetId())
	if err != nil || disabledRole != iamv1.MemberRole_MEMBER_ROLE_UNSPECIFIED {
		t.Fatalf("MemberRoleProvider.Role(disabled) = %v, %v", disabledRole, err)
	}
}

func nextTestEvent(
	t *testing.T,
	harness *enterpriseTestHarness,
	subscription eventbus.Subscription,
) eventbus.Event {
	t.Helper()
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	delivery, err := subscription.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if err := subscription.Ack(context.Background(), delivery); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	return delivery.Event
}

func assertEventPayload(t *testing.T, event eventbus.Event, want map[string]string) {
	t.Helper()
	var got map[string]string
	if err := json.Unmarshal(event.Payload, &got); err != nil {
		t.Fatalf("unmarshal %s payload: %v", event.Header.Type, err)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s payload[%s] = %q, want %q (payload %s)",
				event.Header.Type,
				key,
				got[key],
				value,
				event.Payload,
			)
		}
	}
}

func inviteTestUser(
	t *testing.T,
	client iamv1.IamServiceClient,
	ownerContext context.Context,
	username string,
) *iamv1.Invitation {
	t.Helper()
	response, err := client.InviteMember(ownerContext, &iamv1.InviteMemberRequest{
		UsernameList: []string{username},
	})
	if err != nil {
		t.Fatalf("InviteMember(%q) error = %v", username, err)
	}
	if len(response.GetInvitationList()) != 1 {
		t.Fatalf("InviteMember(%q) invitation count = %d", username, len(response.GetInvitationList()))
	}
	return response.GetInvitationList()[0]
}

func registerTestUser(
	t *testing.T,
	client iamv1.IamServiceClient,
	username, enterpriseName string,
) *iamv1.User {
	t.Helper()
	response, err := client.Register(context.Background(), &iamv1.RegisterRequest{
		Username:       username,
		Password:       "Pass1234",
		EnterpriseName: enterpriseName,
	})
	if err != nil {
		t.Fatalf("Register(%q) error = %v", username, err)
	}
	return response.GetUser()
}

func testPrincipal(user *iamv1.User) *commonv1.Principal {
	return &commonv1.Principal{
		UserId:       user.GetId(),
		EnterpriseId: user.GetEnterpriseId(),
		Internal:     true,
	}
}

func errorReason(err error) string {
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
