package share

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"excel-libs/eventbus"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

func TestLinkMutationsPublishLinkClosedAndManagerGuardsProtectSelf(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID       = "01J00000000000000000003301"
		enterpriseID = "01J00000000000000000003302"
		managerID    = "01J00000000000000000003303"
		token        = "link-event-token"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	seedShareCollaborator(t, harness, baseID, managerID, "manager", true)
	seedShareLink(t, harness, baseID, token, "anyone", "viewer", true)
	managerContext := contextWithSharePrincipal(t, principalForUser(managerID, enterpriseID))

	subscription, err := harness.events.Subscribe(ctx, eventbus.SubscribeOptions{
		Topic:     "link.closed",
		Mode:      eventbus.DeliveryModeReliable,
		QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("subscribe link.closed: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })

	if _, err := harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_ENTERPRISE,
		Role:   sharev1.Role_ROLE_VIEWER,
	}); err != nil {
		t.Fatalf("UpdateLink(narrow scope) error = %v", err)
	}
	assertLinkClosedEvent(t, harness, subscription, baseID, managerID)

	if _, err := harness.client.CloseLink(managerContext, &sharev1.CloseLinkRequest{BaseId: baseID}); err != nil {
		t.Fatalf("CloseLink() error = %v", err)
	}
	assertLinkClosedEvent(t, harness, subscription, baseID, managerID)

	if _, err := harness.client.ResetLink(managerContext, &sharev1.ResetLinkRequest{BaseId: baseID}); err != nil {
		t.Fatalf("ResetLink() error = %v", err)
	}
	assertLinkClosedEvent(t, harness, subscription, baseID, managerID)

	if _, err := harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_ANYONE,
		Role:   sharev1.Role_ROLE_VIEWER,
	}); err != nil {
		t.Fatalf("UpdateLink(reopen) error = %v", err)
	}
	if _, err := harness.client.ForceCloseByManager(
		managerContext,
		&sharev1.ForceCloseByManagerRequest{LinkId: baseID},
	); err != nil {
		t.Fatalf("ForceCloseByManager() error = %v", err)
	}
	assertLinkClosedEvent(t, harness, subscription, baseID, managerID)

	_, err = harness.client.RemoveCollaborator(
		managerContext,
		&sharev1.RemoveCollaboratorRequest{BaseId: baseID, UserId: managerID},
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("RemoveCollaborator(self) error = %v, want 3001", err)
	}
	_, err = harness.client.UpdateCollaboratorRole(
		managerContext,
		&sharev1.UpdateCollaboratorRoleRequest{
			BaseId: baseID,
			UserId: managerID,
			Role:   sharev1.Role_ROLE_VIEWER,
		},
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("UpdateCollaboratorRole(self downgrade) error = %v, want 3001", err)
	}
}

func assertLinkClosedEvent(
	t *testing.T,
	harness *shareTestHarness,
	subscription eventbus.Subscription,
	baseID, operatorID string,
) {
	t.Helper()
	published, err := harness.forwarder.ForwardOnce(context.Background())
	if err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}
	if published != 1 {
		t.Fatalf("ForwardOnce() published = %d, want 1", published)
	}
	receiveContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivery, err := subscription.Receive(receiveContext)
	if err != nil {
		t.Fatalf("receive link.closed: %v", err)
	}
	event := delivery.Event
	if len(event.Header.EventID) != 26 ||
		event.Header.Type != "link.closed" ||
		event.Header.Version != 1 ||
		event.Header.BaseID != baseID ||
		event.Header.Seq != 0 ||
		event.Header.OccurredAt <= 0 ||
		event.Header.Producer != "share" {
		t.Fatalf("link.closed header = %+v", event.Header)
	}
	var payload struct {
		BaseID   string `json:"base_id"`
		LinkID   string `json:"link_id"`
		Operator string `json:"operator"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode link.closed payload: %v", err)
	}
	if payload.BaseID != baseID ||
		payload.LinkID != baseID ||
		payload.Operator != operatorID ||
		payload.Reason == "" {
		t.Fatalf("link.closed payload = %+v", payload)
	}
}

func principalForUser(userID, enterpriseID string) *commonv1.Principal {
	return &commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     true,
	}
}
