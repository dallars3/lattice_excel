package share

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"excel-libs/eventbus"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

func TestCollaboratorGovernanceCapsExternalRolesProtectsManagersAndPublishesChanges(t *testing.T) {
	harness := newShareTestHarnessWithPolicy(t, true)
	const (
		baseID        = "01J00000000000000000003401"
		enterpriseID  = "01J00000000000000000003402"
		managerID     = "01J00000000000000000003403"
		externalID    = "01J00000000000000000003404"
		lastManagerID = "01J00000000000000000003406"
		externalName  = "external-editor"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = managerID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	harness.userLookup[externalName] = shareUserSpec{userID: externalID, internal: false}
	seedShareCollaborator(t, harness, baseID, managerID, "manager", true)
	managerContext := contextWithSharePrincipal(t, principalForUser(managerID, enterpriseID))

	subscription, err := harness.events.Subscribe(context.Background(), eventbus.SubscribeOptions{
		Topic:     "share.changed",
		Mode:      eventbus.DeliveryModeReliable,
		QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("subscribe share.changed: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })

	_, err = harness.client.AddCollaborator(
		managerContext,
		&sharev1.AddCollaboratorRequest{
			BaseId:   baseID,
			Username: externalName,
			Role:     sharev1.Role_ROLE_MANAGER,
		},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("AddCollaborator(external manager) error = %v, want 1001", err)
	}

	added, err := harness.client.AddCollaborator(
		managerContext,
		&sharev1.AddCollaboratorRequest{
			BaseId:   baseID,
			Username: externalName,
			Role:     sharev1.Role_ROLE_EDITOR,
		},
	)
	if err != nil {
		t.Fatalf("AddCollaborator(external editor) error = %v", err)
	}
	if added.GetCollaborator().GetRole() != sharev1.Role_ROLE_EDITOR ||
		added.GetCollaborator().GetInternal() {
		t.Fatalf("AddCollaborator(external editor) = %+v", added.GetCollaborator())
	}
	assertShareChangedEvent(t, harness, subscription, baseID, "added", externalID, "editor")

	_, err = harness.client.UpdateCollaboratorRole(
		managerContext,
		&sharev1.UpdateCollaboratorRoleRequest{
			BaseId: baseID,
			UserId: externalID,
			Role:   sharev1.Role_ROLE_MANAGER,
		},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("UpdateCollaboratorRole(external manager) error = %v, want 1001", err)
	}
	if _, err := harness.client.UpdateCollaboratorRole(
		managerContext,
		&sharev1.UpdateCollaboratorRoleRequest{
			BaseId: baseID,
			UserId: externalID,
			Role:   sharev1.Role_ROLE_EDITOR,
		},
	); err != nil {
		t.Fatalf("UpdateCollaboratorRole(no-op) error = %v", err)
	}
	if published, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce(no-op) error = %v", err)
	} else if published != 0 {
		t.Fatalf("ForwardOnce(no-op) published = %d, want 0", published)
	}
	if _, err := harness.client.UpdateCollaboratorRole(
		managerContext,
		&sharev1.UpdateCollaboratorRoleRequest{
			BaseId: baseID,
			UserId: externalID,
			Role:   sharev1.Role_ROLE_VIEWER,
		},
	); err != nil {
		t.Fatalf("UpdateCollaboratorRole(external viewer) error = %v", err)
	}
	assertShareChangedEvent(t, harness, subscription, baseID, "updated", externalID, "viewer")

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
			Role:   sharev1.Role_ROLE_EDITOR,
		},
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("UpdateCollaboratorRole(self demotion) error = %v, want 3001", err)
	}

	delete(harness.baseOwner, baseID)
	if _, err := harness.db.ExecContext(
		context.Background(),
		"DELETE FROM excel_share_collaborators WHERE base_id = ? AND user_id = ?",
		baseID,
		managerID,
	); err != nil {
		t.Fatalf("remove original manager fixture: %v", err)
	}
	seedShareCollaborator(t, harness, baseID, lastManagerID, "manager", false)
	viewerID := "01J00000000000000000003405"
	harness.memberRole[enterpriseID+"/"+viewerID] = iamv1.MemberRole_MEMBER_ROLE_MEMBER
	seedShareCollaborator(t, harness, baseID, viewerID, "viewer", true)
	viewerContext := contextWithSharePrincipal(t, principalForUser(viewerID, enterpriseID))
	if _, err := harness.client.ListCollaborators(
		viewerContext,
		&sharev1.ListCollaboratorsRequest{BaseId: baseID},
	); err != nil {
		t.Fatalf("ListCollaborators(viewer manage_share) error = %v", err)
	}
	_, err = harness.client.RemoveCollaborator(
		viewerContext,
		&sharev1.RemoveCollaboratorRequest{BaseId: baseID, UserId: lastManagerID},
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("RemoveCollaborator(last manager) error = %v, want 3001", err)
	}

	ownerID := "01J00000000000000000003407"
	harness.baseOwner[baseID] = ownerID
	if _, err := harness.client.RemoveCollaborator(
		viewerContext,
		&sharev1.RemoveCollaboratorRequest{BaseId: baseID, UserId: lastManagerID},
	); err != nil {
		t.Fatalf("RemoveCollaborator(last manager with Base owner) error = %v", err)
	}
	assertShareChangedEvent(t, harness, subscription, baseID, "removed", lastManagerID, "manager")
}

func assertShareChangedEvent(
	t *testing.T,
	harness *shareTestHarness,
	subscription eventbus.Subscription,
	baseID, changeType, userID, role string,
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
		t.Fatalf("receive share.changed: %v", err)
	}
	if delivery.Event.Header.Type != "share.changed" {
		t.Fatalf("share.changed header type = %q", delivery.Event.Header.Type)
	}
	var payload struct {
		BaseID       string `json:"base_id"`
		ChangeType   string `json:"change_type"`
		UserID       string `json:"user_id"`
		Role         string `json:"role"`
		EnterpriseID string `json:"enterprise_id"`
	}
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode share.changed payload: %v", err)
	}
	if payload.BaseID != baseID ||
		payload.ChangeType != changeType ||
		payload.UserID != userID ||
		payload.Role != role ||
		payload.EnterpriseID == "" {
		t.Fatalf("share.changed payload = %+v", payload)
	}
}
