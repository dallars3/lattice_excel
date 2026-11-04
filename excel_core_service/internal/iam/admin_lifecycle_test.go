package iam

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"excel-libs/eventbus"
	iamv1 "excel/api/iam/v1"
)

const testAdminToken = "test-admin-token"

func TestAdminOperationsRejectMissingToken(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	operations := map[string]func(context.Context) error{
		"SetRegisterSwitch": func(ctx context.Context) error {
			_, err := harness.client.SetRegisterSwitch(ctx, &iamv1.SetRegisterSwitchRequest{Enabled: false})
			return err
		},
		"GetRegisterSwitch": func(ctx context.Context) error {
			_, err := harness.client.GetRegisterSwitch(ctx, &emptypb.Empty{})
			return err
		},
		"AdminResetPassword": func(ctx context.Context) error {
			_, err := harness.client.AdminResetPassword(ctx, &iamv1.AdminResetPasswordRequest{
				Username:    "Missing_User",
				NewPassword: "Pass1234",
			})
			return err
		},
		"AdminTransferOwner": func(ctx context.Context) error {
			_, err := harness.client.AdminTransferOwner(ctx, &iamv1.AdminTransferOwnerRequest{
				EnterpriseId: "01J00000000000000000000001",
				NewOwnerId:   "01J00000000000000000000002",
			})
			return err
		},
		"InitiateEnterpriseDissolution": func(ctx context.Context) error {
			_, err := harness.client.InitiateEnterpriseDissolution(
				ctx,
				&iamv1.InitiateEnterpriseDissolutionRequest{
					EnterpriseId: "01J00000000000000000000001",
				},
			)
			return err
		},
	}
	for name, operation := range operations {
		err := operation(context.Background())
		if status.Code(err) != codes.PermissionDenied || errorReason(err) != "3001" {
			t.Fatalf("%s() error = %v, want permission denied with code 3001", name, err)
		}
	}
}

func TestAdminRegisterSwitchPersistsImmediatelyAndAudits(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: "audit.append",
		Mode:  eventbus.DeliveryModeReliable,
		Group: "test-audit",
	})
	if err != nil {
		t.Fatalf("Subscribe(audit.append) error = %v", err)
	}
	defer subscription.Close()

	adminContext := metadata.AppendToOutgoingContext(
		context.Background(),
		"x-admin-token",
		testAdminToken,
	)
	if _, err := harness.client.SetRegisterSwitch(adminContext, &iamv1.SetRegisterSwitchRequest{
		Enabled: false,
	}); err != nil {
		t.Fatalf("SetRegisterSwitch() error = %v", err)
	}
	switchState, err := harness.client.GetRegisterSwitch(adminContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetRegisterSwitch() error = %v", err)
	}
	if switchState.GetEnabled() {
		t.Fatal("GetRegisterSwitch().enabled = true, want false")
	}

	event := nextTestEvent(t, harness, subscription)
	var payload struct {
		Action    string         `json:"action"`
		Operator  string         `json:"operator"`
		Target    string         `json:"target"`
		DetailMap map[string]any `json:"detail_map"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal audit payload: %v", err)
	}
	if payload.Action != "AUDIT_ACTION_REGISTER_SWITCH_CHANGED" ||
		payload.Operator != "admin" ||
		payload.Target != "register_switch" ||
		payload.DetailMap["enabled"] != false {
		t.Fatalf("audit.append payload = %+v", payload)
	}
}

func TestRegisterSwitchClosesOnlyNewRegistrationPaths(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	adminContext := metadata.AppendToOutgoingContext(
		context.Background(),
		"x-admin-token",
		testAdminToken,
	)
	if _, err := harness.client.SetRegisterSwitch(adminContext, &iamv1.SetRegisterSwitchRequest{
		Enabled: false,
	}); err != nil {
		t.Fatalf("SetRegisterSwitch() error = %v", err)
	}

	for name, request := range map[string]*iamv1.RegisterRequest{
		"link": {
			Username:  "Closed_Link",
			Password:  "Pass1234",
			LinkToken: "closed-link-token",
		},
		"enterprise": {
			Username:       "Closed_Owner",
			Password:       "Pass1234",
			EnterpriseName: "关闭注册企业",
		},
	} {
		_, err := harness.client.Register(context.Background(), request)
		if status.Code(err) != codes.InvalidArgument ||
			status.Convert(err).Message() != "当前未开放新注册" {
			t.Fatalf("Register(%s) error = %v, want 1001 当前未开放新注册", name, err)
		}
	}

	if _, err := harness.client.Register(context.Background(), &iamv1.RegisterRequest{
		Username: "Invited_Path",
		Password: "Pass1234",
	}); err != nil {
		t.Fatalf("Register(path 1) error = %v", err)
	}
}

func TestAdminEmergencyActionsTakeEffectAndAudit(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	owner := registerTestUser(t, harness.client, "Admin_Owner", "运营应急企业")
	joiner := registerTestUser(t, harness.client, "Admin_Joiner", "")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	joinerContext := contextWithPrincipal(t, testPrincipal(joiner))
	invitation := inviteTestUser(t, harness.client, ownerContext, joiner.GetUsername())
	if _, err := harness.client.AcceptInvite(joinerContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: "audit.append",
		Mode:  eventbus.DeliveryModeReliable,
		Group: "test-admin-actions",
	})
	if err != nil {
		t.Fatalf("Subscribe(audit.append) error = %v", err)
	}
	defer subscription.Close()
	adminContext := metadata.AppendToOutgoingContext(
		context.Background(),
		"x-admin-token",
		testAdminToken,
	)

	if _, err := harness.client.AdminResetPassword(adminContext, &iamv1.AdminResetPasswordRequest{
		Username:    owner.GetUsername(),
		NewPassword: "Reset1234",
	}); err != nil {
		t.Fatalf("AdminResetPassword() error = %v", err)
	}
	resetAudit := nextTestEvent(t, harness, subscription)
	assertAuditAction(t, resetAudit, "AUDIT_ACTION_ADMIN_RESET_PASSWORD", owner.GetUsername())
	if _, err := harness.client.Login(context.Background(), &iamv1.LoginRequest{
		Username: owner.GetUsername(),
		Password: "Reset1234",
	}); err != nil {
		t.Fatalf("Login(reset password) error = %v", err)
	}

	if _, err := harness.client.AdminTransferOwner(adminContext, &iamv1.AdminTransferOwnerRequest{
		EnterpriseId: owner.GetEnterpriseId(),
		NewOwnerId:   joiner.GetId(),
	}); err != nil {
		t.Fatalf("AdminTransferOwner() error = %v", err)
	}
	transferAudit := nextTestEvent(t, harness, subscription)
	assertAuditAction(
		t,
		transferAudit,
		"AUDIT_ACTION_BASE_OWNERSHIP_TRANSFERRED",
		owner.GetEnterpriseId(),
	)
	me, err := harness.client.GetMe(joinerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMe(new owner) error = %v", err)
	}
	if me.GetMyRole() != "owner" {
		t.Fatalf("GetMe(new owner).my_role = %q, want owner", me.GetMyRole())
	}
}

func assertAuditAction(t *testing.T, event eventbus.Event, action, target string) {
	t.Helper()
	var payload struct {
		Action   string `json:"action"`
		Operator string `json:"operator"`
		Target   string `json:"target"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal audit payload: %v", err)
	}
	if payload.Action != action || payload.Operator != "admin" || payload.Target != target {
		t.Fatalf("audit payload = %+v, want action=%s operator=admin target=%s", payload, action, target)
	}
}

type lifecycleClock struct {
	now time.Time
}

func (clock *lifecycleClock) Now() time.Time {
	return clock.now
}

func (clock *lifecycleClock) Advance(duration time.Duration) {
	clock.now = clock.now.Add(duration)
}

func TestEnterpriseDissolutionLifecycleAndCooldown(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	owner := registerTestUser(t, harness.client, "Dissolve_Owner", "注销状态企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	secondOwner := registerTestUser(t, harness.client, "Dissolve_Second", "到期执行企业")
	secondOwnerContext := contextWithPrincipal(t, testPrincipal(secondOwner))
	clock := &lifecycleClock{now: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	harness.service.now = clock.Now

	initial, err := harness.client.GetEnterpriseDissolutionStatus(ownerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterpriseDissolutionStatus(none) error = %v", err)
	}
	if initial.GetStatus() != iamv1.DissolutionStatus_DISSOLUTION_STATUS_NONE {
		t.Fatalf("initial dissolution status = %v, want none", initial.GetStatus())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: "audit.append",
		Mode:  eventbus.DeliveryModeReliable,
		Group: "test-dissolution",
	})
	if err != nil {
		t.Fatalf("Subscribe(audit.append) error = %v", err)
	}
	defer subscription.Close()
	adminContext := metadata.AppendToOutgoingContext(
		context.Background(),
		"x-admin-token",
		testAdminToken,
	)
	initiated, err := harness.client.InitiateEnterpriseDissolution(
		adminContext,
		&iamv1.InitiateEnterpriseDissolutionRequest{
			EnterpriseId: owner.GetEnterpriseId(),
			Note:         "运营核验通过",
		},
	)
	if err != nil {
		t.Fatalf("InitiateEnterpriseDissolution() error = %v", err)
	}
	wantDeadline := clock.now.Add(30 * 24 * time.Hour).UnixMilli()
	if initiated.GetDeadlineMs() != wantDeadline {
		t.Fatalf("deadline = %d, want %d", initiated.GetDeadlineMs(), wantDeadline)
	}
	pending, err := harness.client.GetEnterpriseDissolutionStatus(ownerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterpriseDissolutionStatus(pending) error = %v", err)
	}
	if pending.GetStatus() != iamv1.DissolutionStatus_DISSOLUTION_STATUS_PENDING ||
		pending.GetDeadlineMs() != wantDeadline {
		t.Fatalf("pending status = %v deadline=%d", pending.GetStatus(), pending.GetDeadlineMs())
	}
	acceptedAudit := nextTestEvent(t, harness, subscription)
	assertAuditAction(
		t,
		acceptedAudit,
		"AUDIT_ACTION_ENTERPRISE_DISSOLUTION_ACCEPTED",
		owner.GetEnterpriseId(),
	)

	clock.Advance(29 * 24 * time.Hour)
	if _, err := harness.client.CancelEnterpriseDissolution(ownerContext, &emptypb.Empty{}); err != nil {
		t.Fatalf("CancelEnterpriseDissolution() error = %v", err)
	}
	revoked, err := harness.client.GetEnterpriseDissolutionStatus(ownerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterpriseDissolutionStatus(revoked) error = %v", err)
	}
	if revoked.GetStatus() != iamv1.DissolutionStatus_DISSOLUTION_STATUS_REVOKED {
		t.Fatalf("revoked status = %v, want revoked", revoked.GetStatus())
	}
	if _, err := harness.client.InitiateEnterpriseDissolution(
		adminContext,
		&iamv1.InitiateEnterpriseDissolutionRequest{EnterpriseId: owner.GetEnterpriseId()},
	); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("early repeat Initiate() error = %v, want PermissionDenied", err)
	}
	clock.Advance(24*time.Hour + time.Millisecond)
	if _, err := harness.client.InitiateEnterpriseDissolution(
		adminContext,
		&iamv1.InitiateEnterpriseDissolutionRequest{EnterpriseId: owner.GetEnterpriseId()},
	); err != nil {
		t.Fatalf("Initiate() after revoked cooldown error = %v", err)
	}

	secondInitiated, err := harness.client.InitiateEnterpriseDissolution(
		adminContext,
		&iamv1.InitiateEnterpriseDissolutionRequest{EnterpriseId: secondOwner.GetEnterpriseId()},
	)
	if err != nil {
		t.Fatalf("InitiateEnterpriseDissolution(second) error = %v", err)
	}
	clock.now = time.UnixMilli(secondInitiated.GetDeadlineMs())
	cooling, err := harness.client.GetEnterpriseDissolutionStatus(secondOwnerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterpriseDissolutionStatus(cooling) error = %v", err)
	}
	if cooling.GetStatus() != iamv1.DissolutionStatus_DISSOLUTION_STATUS_COOLING {
		t.Fatalf("cooling status = %v, want cooling", cooling.GetStatus())
	}
	dissolvedSubscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: "enterprise.dissolved",
		Mode:  eventbus.DeliveryModeReliable,
		Group: "test-dissolved",
	})
	if err != nil {
		t.Fatalf("Subscribe(enterprise.dissolved) error = %v", err)
	}
	defer dissolvedSubscription.Close()
	if _, err := harness.client.ExecuteEnterpriseDissolution(
		context.Background(),
		&iamv1.ExecuteEnterpriseDissolutionRequest{EnterpriseId: secondOwner.GetEnterpriseId()},
	); err != nil {
		t.Fatalf("ExecuteEnterpriseDissolution() error = %v", err)
	}
	dissolvedEvent := nextTestEvent(t, harness, dissolvedSubscription)
	var dissolvedPayload struct {
		EnterpriseID string   `json:"enterprise_id"`
		BaseIDList   []string `json:"base_id_list"`
		MemberIDs    []string `json:"members_ids"`
	}
	if err := json.Unmarshal(dissolvedEvent.Payload, &dissolvedPayload); err != nil {
		t.Fatalf("unmarshal enterprise.dissolved payload: %v", err)
	}
	if dissolvedPayload.EnterpriseID != secondOwner.GetEnterpriseId() ||
		len(dissolvedPayload.BaseIDList) != 1 ||
		dissolvedPayload.BaseIDList[0] != harness.baseLister.baseIDs[0] ||
		len(dissolvedPayload.MemberIDs) != 1 ||
		dissolvedPayload.MemberIDs[0] != secondOwner.GetId() {
		t.Fatalf("enterprise.dissolved payload = %+v", dissolvedPayload)
	}
	done, err := harness.client.GetEnterpriseDissolutionStatus(secondOwnerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterpriseDissolutionStatus(done) error = %v", err)
	}
	if done.GetStatus() != iamv1.DissolutionStatus_DISSOLUTION_STATUS_DONE {
		t.Fatalf("done status = %v, want done", done.GetStatus())
	}
}

func TestUserDeactivationCooldownAndAssetHandoff(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	owner := registerTestUser(t, harness.client, "Deactivate_Owner", "注销账号企业")
	member := registerTestUser(t, harness.client, "Deactivate_User", "")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	memberContext := contextWithPrincipal(t, testPrincipal(member))
	invitation := inviteTestUser(t, harness.client, ownerContext, member.GetUsername())
	if _, err := harness.client.AcceptInvite(memberContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}
	clock := &lifecycleClock{now: time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC)}
	harness.service.now = clock.Now

	requested, err := harness.client.RequestDeactivate(memberContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("RequestDeactivate() error = %v", err)
	}
	wantDeadline := clock.now.Add(7 * 24 * time.Hour).UnixMilli()
	if requested.GetDeadlineMs() != wantDeadline {
		t.Fatalf("deactivation deadline = %d, want %d", requested.GetDeadlineMs(), wantDeadline)
	}
	if _, err := harness.client.CancelDeactivate(memberContext, &emptypb.Empty{}); err != nil {
		t.Fatalf("CancelDeactivate() error = %v", err)
	}
	requested, err = harness.client.RequestDeactivate(memberContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("RequestDeactivate() after cancel error = %v", err)
	}

	clock.now = time.UnixMilli(requested.GetDeadlineMs()).Add(-time.Millisecond)
	if _, err := harness.client.ExecuteDeactivation(
		context.Background(),
		&iamv1.ExecuteDeactivationRequest{UserId: member.GetId()},
	); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("early ExecuteDeactivation() error = %v, want PermissionDenied", err)
	}
	clock.Advance(time.Millisecond)
	if _, err := harness.client.ExecuteDeactivation(
		context.Background(),
		&iamv1.ExecuteDeactivationRequest{UserId: member.GetId()},
	); err != nil {
		t.Fatalf("ExecuteDeactivation() error = %v", err)
	}
	if _, err := harness.client.Login(context.Background(), &iamv1.LoginRequest{
		Username: member.GetUsername(),
		Password: "Pass1234",
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Login(deactivated account) error = %v, want Unauthenticated", err)
	}
	if len(harness.bases.fromUserIDs) != 1 ||
		harness.bases.fromUserIDs[0] != member.GetId() ||
		harness.bases.toUserIDs[0] != owner.GetId() {
		t.Fatalf(
			"asset handoff = from %v to %v, want once from %s to %s",
			harness.bases.fromUserIDs,
			harness.bases.toUserIDs,
			member.GetId(),
			owner.GetId(),
		)
	}
}

func TestDeactivationInitiatedEventEmittedOnce(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	owner := registerTestUser(t, harness.client, "Intake_Owner", "事件企业")
	member := registerTestUser(t, harness.client, "Intake_User", "")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	memberContext := contextWithPrincipal(t, testPrincipal(member))
	invitation := inviteTestUser(t, harness.client, ownerContext, member.GetUsername())
	if _, err := harness.client.AcceptInvite(memberContext, &iamv1.AcceptInviteRequest{
		InviteId: invitation.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}
	clock := &lifecycleClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	harness.service.now = clock.Now

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: "deactivation.initiated",
		Mode:  eventbus.DeliveryModeReliable,
		Group: "test-deactivation-intake",
	})
	if err != nil {
		t.Fatalf("Subscribe(deactivation.initiated) error = %v", err)
	}
	defer subscription.Close()

	requested, err := harness.client.RequestDeactivate(memberContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("RequestDeactivate() error = %v", err)
	}
	wantDeadline := clock.now.Add(7 * 24 * time.Hour).UnixMilli()
	if requested.GetDeadlineMs() != wantDeadline {
		t.Fatalf("deadline = %d, want %d", requested.GetDeadlineMs(), wantDeadline)
	}
	event := nextTestEvent(t, harness, subscription)
	var payload struct {
		EntityID     string `json:"entity_id"`
		Kind         string `json:"kind"`
		DeadlineMs   int64  `json:"deadline_ms"`
		Operator     string `json:"operator"`
		EnterpriseID string `json:"enterprise_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal deactivation.initiated payload: %v", err)
	}
	if payload.EntityID != member.GetId() ||
		payload.Kind != "user" ||
		payload.DeadlineMs != wantDeadline ||
		payload.Operator != member.GetId() ||
		payload.EnterpriseID != owner.GetEnterpriseId() {
		t.Fatalf("deactivation.initiated payload = %+v", payload)
	}

	repeated, err := harness.client.RequestDeactivate(memberContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("repeat RequestDeactivate() error = %v", err)
	}
	if repeated.GetDeadlineMs() != wantDeadline {
		t.Fatalf("repeat deadline = %d, want %d", repeated.GetDeadlineMs(), wantDeadline)
	}
	assertNoEvent(t, subscription)
}

func TestDissolutionInitiatedEventEmitted(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	owner := registerTestUser(t, harness.client, "Intake_Owner2", "发起企业")
	clock := &lifecycleClock{now: time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)}
	harness.service.now = clock.Now

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic: "dissolution.initiated",
		Mode:  eventbus.DeliveryModeReliable,
		Group: "test-dissolution-intake",
	})
	if err != nil {
		t.Fatalf("Subscribe(dissolution.initiated) error = %v", err)
	}
	defer subscription.Close()
	adminContext := metadata.AppendToOutgoingContext(
		context.Background(),
		"x-admin-token",
		testAdminToken,
	)

	initiated, err := harness.client.InitiateEnterpriseDissolution(
		adminContext,
		&iamv1.InitiateEnterpriseDissolutionRequest{EnterpriseId: owner.GetEnterpriseId()},
	)
	if err != nil {
		t.Fatalf("InitiateEnterpriseDissolution() error = %v", err)
	}
	wantDeadline := clock.now.Add(30 * 24 * time.Hour).UnixMilli()
	if initiated.GetDeadlineMs() != wantDeadline {
		t.Fatalf("deadline = %d, want %d", initiated.GetDeadlineMs(), wantDeadline)
	}
	event := nextTestEvent(t, harness, subscription)
	var payload struct {
		EntityID   string `json:"entity_id"`
		Kind       string `json:"kind"`
		DeadlineMs int64  `json:"deadline_ms"`
		Operator   string `json:"operator"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal dissolution.initiated payload: %v", err)
	}
	if payload.EntityID != owner.GetEnterpriseId() ||
		payload.Kind != "enterprise" ||
		payload.DeadlineMs != wantDeadline ||
		payload.Operator != "admin" {
		t.Fatalf("dissolution.initiated payload = %+v", payload)
	}
}

func TestExecuteDeactivationAfterCancelIsNoop(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	member := registerTestUser(t, harness.client, "Withdraw_User", "")
	memberContext := contextWithPrincipal(t, testPrincipal(member))
	clock := &lifecycleClock{now: time.Date(2026, 11, 4, 13, 0, 0, 0, time.UTC)}
	harness.service.now = clock.Now

	requested, err := harness.client.RequestDeactivate(memberContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("RequestDeactivate() error = %v", err)
	}
	if _, err := harness.client.CancelDeactivate(memberContext, &emptypb.Empty{}); err != nil {
		t.Fatalf("CancelDeactivate() error = %v", err)
	}
	clock.now = time.UnixMilli(requested.GetDeadlineMs())
	if _, err := harness.client.ExecuteDeactivation(
		context.Background(),
		&iamv1.ExecuteDeactivationRequest{UserId: member.GetId()},
	); err != nil {
		t.Fatalf("ExecuteDeactivation(after cancel) error = %v, want no-op", err)
	}
	if _, err := harness.client.Login(context.Background(), &iamv1.LoginRequest{
		Username: member.GetUsername(),
		Password: "Pass1234",
	}); err != nil {
		t.Fatalf("Login(after withdrawn) error = %v, want success", err)
	}
}

func TestExecuteEnterpriseDissolutionAfterRevokeIsNoop(t *testing.T) {
	harness := newEnterpriseTestHarness(t)
	owner := registerTestUser(t, harness.client, "Revoke_Owner", "撤回企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	clock := &lifecycleClock{now: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	harness.service.now = clock.Now
	adminContext := metadata.AppendToOutgoingContext(
		context.Background(),
		"x-admin-token",
		testAdminToken,
	)

	initiated, err := harness.client.InitiateEnterpriseDissolution(
		adminContext,
		&iamv1.InitiateEnterpriseDissolutionRequest{EnterpriseId: owner.GetEnterpriseId()},
	)
	if err != nil {
		t.Fatalf("InitiateEnterpriseDissolution() error = %v", err)
	}
	if _, err := harness.client.CancelEnterpriseDissolution(ownerContext, &emptypb.Empty{}); err != nil {
		t.Fatalf("CancelEnterpriseDissolution() error = %v", err)
	}
	clock.now = time.UnixMilli(initiated.GetDeadlineMs())
	if _, err := harness.client.ExecuteEnterpriseDissolution(
		context.Background(),
		&iamv1.ExecuteEnterpriseDissolutionRequest{EnterpriseId: owner.GetEnterpriseId()},
	); err != nil {
		t.Fatalf("ExecuteEnterpriseDissolution(after revoke) error = %v, want no-op", err)
	}
	status, err := harness.client.GetEnterpriseDissolutionStatus(ownerContext, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetEnterpriseDissolutionStatus() error = %v", err)
	}
	if status.GetStatus() != iamv1.DissolutionStatus_DISSOLUTION_STATUS_REVOKED {
		t.Fatalf("status after revoke = %v, want revoked", status.GetStatus())
	}
}

func assertNoEvent(t *testing.T, subscription eventbus.Subscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	delivery, err := subscription.Receive(ctx)
	if err == nil {
		t.Fatalf("expected no additional event, got %s", delivery.Event.Header.Type)
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("Receive() unexpected error = %v", err)
	}
}
