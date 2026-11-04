package share

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"excel-libs/eventbus"
	auditv1 "excel/api/audit/v1"
	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
	"excel_core_service/bus"
	"excel_core_service/internal/audit"
)

// TestForceCloseByManagerProducesSingleAuditRecord verifies that the
// link.closed mapping path and the explicit audit.append path share one
// audit idempotency key (audit_event_id), so the audit store keeps exactly
// one LINK_FORCE_CLOSED row.
func TestForceCloseByManagerProducesSingleAuditRecord(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID          = "01J00000000000000000003701"
		enterpriseID    = "01J00000000000000000003702"
		enterpriseMgrID = "01J00000000000000000003703"
		baseOwnerID     = "01J00000000000000000003704"
		token           = "force-close-audit-dedup-token"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = baseOwnerID
	harness.memberRole[enterpriseID+"/"+enterpriseMgrID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	seedShareLink(t, harness, baseID, token, "anyone", "viewer", true)

	if _, err := harness.db.ExecContext(
		context.Background(),
		"DELETE FROM excel_audit_events WHERE enterprise_id = ?",
		enterpriseID,
	); err != nil {
		t.Fatalf("cleanup audit events: %v", err)
	}
	t.Cleanup(func() {
		_, _ = harness.db.ExecContext(
			context.Background(),
			"DELETE FROM excel_audit_events WHERE enterprise_id = ?",
			enterpriseID,
		)
	})

	auditService := audit.NewService(
		base.NewAuditRepository(harness.db),
		harness.memberRole,
	)
	consumer := bus.NewConsumer(base.NewBusStore(harness.db), harness.events, bus.ConsumerConfig{
		Retry: eventbus.RetryPolicy{
			Initial:     10 * time.Millisecond,
			Max:         20 * time.Millisecond,
			MaxAttempts: 3,
		},
	})
	if err := audit.Consumers(consumer, auditService); err != nil {
		t.Fatalf("audit.Consumers() error = %v", err)
	}
	consumerContext, cancelConsumer := context.WithCancel(context.Background())
	if err := consumer.Start(consumerContext); err != nil {
		cancelConsumer()
		t.Fatalf("consumer.Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = consumer.Stop(stopContext)
		cancelConsumer()
	})

	managerContext := contextWithSharePrincipal(t, principalForUser(enterpriseMgrID, enterpriseID))
	if _, err := harness.client.ForceCloseByManager(
		managerContext,
		&sharev1.ForceCloseByManagerRequest{LinkId: baseID},
	); err != nil {
		t.Fatalf("ForceCloseByManager() error = %v", err)
	}
	if _, err := harness.forwarder.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}

	queryContext := auditIncomingContext(t, principalForUser(enterpriseMgrID, enterpriseID))
	deadline := time.Now().Add(3 * time.Second)
	var forceClosed []*auditv1.AuditEvent
	for time.Now().Before(deadline) {
		response, err := auditService.Query(queryContext, &auditv1.QueryRequest{Limit: 100})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		forceClosed = forceClosed[:0]
		for _, event := range response.GetAuditList() {
			if event.GetAction() == auditv1.AuditAction_AUDIT_ACTION_LINK_FORCE_CLOSED {
				forceClosed = append(forceClosed, event)
			}
		}
		if len(forceClosed) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(forceClosed) != 1 {
		t.Fatalf("LINK_FORCE_CLOSED audit count = %d, want 1; records = %+v", len(forceClosed), forceClosed)
	}
	got := forceClosed[0]
	if got.GetOperator() != enterpriseMgrID ||
		got.GetTarget() != baseID ||
		got.GetBaseId() != baseID {
		t.Fatalf("LINK_FORCE_CLOSED audit record = %+v", got)
	}
}

func auditIncomingContext(t *testing.T, principal *commonv1.Principal) context.Context {
	t.Helper()
	payload, err := proto.Marshal(principal)
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"x-principal",
			base64.StdEncoding.EncodeToString(payload),
		),
	)
}