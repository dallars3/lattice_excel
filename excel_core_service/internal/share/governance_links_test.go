package share

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"excel-libs/eventbus"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
)

func TestLinkGovernancePublishesExactClosureReasonsPreservesReopenAndRotatesResetToken(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000003501"
		enterpriseID = "01J00000000000000000003502"
		managerID    = "01J00000000000000000003503"
		oldToken     = "governance-old-token"
		oldURL       = "/s/governance-old-token"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = managerID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	seedShareCollaborator(t, harness, baseID, managerID, "manager", true)
	seedShareLink(t, harness, baseID, oldToken, "anyone", "viewer", true)
	managerContext := contextWithSharePrincipal(t, principalForUser(managerID, enterpriseID))

	subscription, err := harness.events.Subscribe(context.Background(), eventbus.SubscribeOptions{
		Topic:     "link.closed",
		Mode:      eventbus.DeliveryModeReliable,
		QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("subscribe link.closed: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	changedSubscription, err := harness.events.Subscribe(context.Background(), eventbus.SubscribeOptions{
		Topic:     "share.changed",
		Mode:      eventbus.DeliveryModeReliable,
		QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("subscribe share.changed: %v", err)
	}
	t.Cleanup(func() { _ = changedSubscription.Close() })

	_, err = harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_ANYONE,
		Role:   sharev1.Role_ROLE_EDITOR,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("UpdateLink(anyone/editor) error = %v, want 1001", err)
	}

	if _, err := harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_ENTERPRISE,
		Role:   sharev1.Role_ROLE_VIEWER,
	}); err != nil {
		t.Fatalf("UpdateLink(anyone to enterprise) error = %v", err)
	}
	assertLinkClosedReason(t, harness, subscription, baseID, managerID, "narrowing")
	assertShareChangedDelivery(
		t,
		changedSubscription,
		baseID,
		enterpriseID,
		"link",
		managerID,
		"viewer",
	)

	if _, err := harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_NONE,
		Role:   sharev1.Role_ROLE_VIEWER,
	}); err != nil {
		t.Fatalf("UpdateLink(close) error = %v", err)
	}
	assertLinkClosedReason(t, harness, subscription, baseID, managerID, "closed")
	assertShareChangedDelivery(
		t,
		changedSubscription,
		baseID,
		enterpriseID,
		"link",
		managerID,
		"viewer",
	)

	if _, err := harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_ANYONE,
		Role:   sharev1.Role_ROLE_VIEWER,
	}); err != nil {
		t.Fatalf("UpdateLink(reopen) error = %v", err)
	}
	reopened, err := harness.client.GetLink(
		managerContext,
		&sharev1.GetLinkRequest{BaseId: baseID},
	)
	if err != nil {
		t.Fatalf("GetLink(reopened) error = %v", err)
	}
	if !reopened.GetLinkConfig().GetEnabled() ||
		reopened.GetLinkConfig().GetUrl() != oldURL {
		t.Fatalf("reopened link config = %+v, want enabled URL %q", reopened.GetLinkConfig(), oldURL)
	}
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce(reopen) error = %v", err)
	}
	assertShareChangedDelivery(
		t,
		changedSubscription,
		baseID,
		enterpriseID,
		"link",
		managerID,
		"viewer",
	)
	assertNoLinkClosedDelivery(t, subscription)

	if _, err := harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_ANYONE,
		Role:   sharev1.Role_ROLE_FILLER,
	}); err != nil {
		t.Fatalf("UpdateLink(role-only) error = %v", err)
	}
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce(role-only) error = %v", err)
	}
	assertShareChangedDelivery(
		t,
		changedSubscription,
		baseID,
		enterpriseID,
		"link",
		managerID,
		"filler",
	)
	assertNoLinkClosedDelivery(t, subscription)

	reset, err := harness.client.ResetLink(
		managerContext,
		&sharev1.ResetLinkRequest{BaseId: baseID},
	)
	if err != nil {
		t.Fatalf("ResetLink() error = %v", err)
	}
	if reset.GetLinkConfig().GetUrl() == "" ||
		reset.GetLinkConfig().GetUrl() == oldURL {
		t.Fatalf("ResetLink() URL = %q, want a new URL", reset.GetLinkConfig().GetUrl())
	}
	resolver := NewLinkResolver(base.NewShareRepository(harness.db))
	if _, err := resolver.ResolveLinkToken(context.Background(), oldToken); !errors.Is(err, ErrShareLinkUnavailable) {
		t.Fatalf("ResolveLinkToken(old token) error = %v, want ErrShareLinkUnavailable", err)
	}
	assertLinkClosedReason(t, harness, subscription, baseID, managerID, "reset")
}

func TestResetLinkAllowsWorkspaceOwnerWithoutExplicitCollaborator(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000003511"
		enterpriseID = "01J00000000000000000003512"
		ownerID      = "01J00000000000000000003513"
		oldToken     = "owner-reset-token"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = ownerID
	harness.memberRole[enterpriseID+"/"+ownerID] = iamv1.MemberRole_MEMBER_ROLE_OWNER
	seedShareLink(t, harness, baseID, oldToken, "anyone", "viewer", true)

	if _, err := harness.client.ResetLink(
		contextWithSharePrincipal(t, principalForUser(ownerID, enterpriseID)),
		&sharev1.ResetLinkRequest{BaseId: baseID},
	); err != nil {
		t.Fatalf("ResetLink(Base owner without collaborator) error = %v", err)
	}
	if _, err := NewLinkResolver(base.NewShareRepository(harness.db)).
		ResolveLinkToken(context.Background(), oldToken); !errors.Is(err, ErrShareLinkUnavailable) {
		t.Fatalf("ResolveLinkToken(old owner token) error = %v", err)
	}
}

func assertShareChangedDelivery(
	t *testing.T,
	subscription eventbus.Subscription,
	baseID, enterpriseID, changeType, userID, role string,
) {
	t.Helper()
	receiveContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivery, err := subscription.Receive(receiveContext)
	if err != nil {
		t.Fatalf("receive share.changed(%s): %v", changeType, err)
	}
	var payload struct {
		BaseID       string `json:"base_id"`
		EnterpriseID string `json:"enterprise_id"`
		ChangeType   string `json:"change_type"`
		UserID       string `json:"user_id"`
		Role         string `json:"role"`
	}
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode share.changed payload: %v", err)
	}
	if payload.BaseID != baseID ||
		payload.EnterpriseID != enterpriseID ||
		payload.ChangeType != changeType ||
		payload.UserID != userID ||
		payload.Role != role {
		t.Fatalf("share.changed payload = %+v", payload)
	}
}

func assertNoLinkClosedDelivery(t *testing.T, subscription eventbus.Subscription) {
	t.Helper()
	receiveContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if delivery, err := subscription.Receive(receiveContext); err == nil {
		t.Fatalf("unexpected link.closed delivery = %+v", delivery.Event)
	}
}

func assertLinkClosedReason(
	t *testing.T,
	harness *shareTestHarness,
	subscription eventbus.Subscription,
	baseID, operatorID, reason string,
) {
	t.Helper()
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}
	receiveContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivery, err := subscription.Receive(receiveContext)
	if err != nil {
		t.Fatalf("receive link.closed(%s): %v", reason, err)
	}
	var payload struct {
		BaseID       string `json:"base_id"`
		LinkID       string `json:"link_id"`
		Operator     string `json:"operator"`
		Reason       string `json:"reason"`
		EnterpriseID string `json:"enterprise_id"`
	}
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode link.closed payload: %v", err)
	}
	if payload.BaseID != baseID ||
		payload.LinkID != baseID ||
		payload.Operator != operatorID ||
		payload.Reason != reason ||
		payload.EnterpriseID == "" {
		t.Fatalf("link.closed payload = %+v, want reason %q", payload, reason)
	}
}
