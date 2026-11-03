package share

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"excel-libs/eventbus"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
)

func TestForceCloseByManagerNotifiesOwnerAndPublishesAuditWithoutBaseRole(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID            = "01J00000000000000000003601"
		enterpriseID      = "01J00000000000000000003602"
		enterpriseMgrID   = "01J00000000000000000003603"
		baseOwnerID       = "01J00000000000000000003604"
		otherEnterpriseID = "01J00000000000000000003605"
		token             = "force-close-governance-token"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = baseOwnerID
	harness.memberRole[enterpriseID+"/"+enterpriseMgrID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	harness.memberRole[otherEnterpriseID+"/"+enterpriseMgrID] = iamv1.MemberRole_MEMBER_ROLE_OWNER
	seedShareLink(t, harness, baseID, token, "anyone", "viewer", true)
	managerContext := contextWithSharePrincipal(t, principalForUser(enterpriseMgrID, enterpriseID))

	linkSubscription, err := harness.events.Subscribe(context.Background(), eventbus.SubscribeOptions{
		Topic:     "link.closed",
		Mode:      eventbus.DeliveryModeReliable,
		QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("subscribe link.closed: %v", err)
	}
	t.Cleanup(func() { _ = linkSubscription.Close() })
	auditSubscription, err := harness.events.Subscribe(context.Background(), eventbus.SubscribeOptions{
		Topic:     "audit.append",
		Mode:      eventbus.DeliveryModeReliable,
		QueueSize: 8,
	})
	if err != nil {
		t.Fatalf("subscribe audit.append: %v", err)
	}
	t.Cleanup(func() { _ = auditSubscription.Close() })

	if _, err := harness.client.ForceCloseByManager(
		managerContext,
		&sharev1.ForceCloseByManagerRequest{LinkId: baseID},
	); err != nil {
		t.Fatalf("ForceCloseByManager() error = %v", err)
	}
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}

	linkContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	linkDelivery, err := linkSubscription.Receive(linkContext)
	if err != nil {
		t.Fatalf("receive link.closed: %v", err)
	}
	var linkPayload struct {
		BaseID        string `json:"base_id"`
		LinkID        string `json:"link_id"`
		EnterpriseID  string `json:"enterprise_id"`
		Operator      string `json:"operator"`
		Reason        string `json:"reason"`
		ManagerUserID string `json:"manager_user_id"`
		CreatorID     string `json:"creator_id"`
	}
	if err := json.Unmarshal(linkDelivery.Event.Payload, &linkPayload); err != nil {
		t.Fatalf("decode link.closed payload: %v", err)
	}
	if linkPayload.BaseID != baseID ||
		linkPayload.LinkID != baseID ||
		linkPayload.EnterpriseID != enterpriseID ||
		linkPayload.Operator != enterpriseMgrID ||
		linkPayload.Reason != "manager_force_close" ||
		linkPayload.ManagerUserID != enterpriseMgrID ||
		linkPayload.CreatorID != baseOwnerID {
		t.Fatalf("link.closed payload = %+v", linkPayload)
	}

	auditContext, auditCancel := context.WithTimeout(context.Background(), time.Second)
	defer auditCancel()
	auditDelivery, err := auditSubscription.Receive(auditContext)
	if err != nil {
		t.Fatalf("receive audit.append: %v", err)
	}
	var auditPayload struct {
		EnterpriseID string         `json:"enterprise_id"`
		Action       string         `json:"action"`
		Operator     string         `json:"operator"`
		Target       string         `json:"target"`
		DetailMap    map[string]any `json:"detail_map"`
	}
	if err := json.Unmarshal(auditDelivery.Event.Payload, &auditPayload); err != nil {
		t.Fatalf("decode audit.append payload: %v", err)
	}
	if auditPayload.EnterpriseID != enterpriseID ||
		auditPayload.Action != "AUDIT_ACTION_LINK_FORCE_CLOSED" ||
		auditPayload.Operator != enterpriseMgrID ||
		auditPayload.Target != baseID ||
		auditPayload.DetailMap["enterprise_id"] != enterpriseID {
		t.Fatalf("audit.append payload = %+v", auditPayload)
	}

	resolver := NewLinkResolver(base.NewShareRepository(harness.db))
	if _, err := resolver.ResolveLinkToken(context.Background(), token); err == nil {
		t.Fatal("ResolveLinkToken(force-closed token) succeeded")
	}
}
