package workspace_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	workspacev1 "excel/api/workspace/v1"

	"excel_core_service/base"
)

func TestDeleteBaseCascadesAndAppendsAudit(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ctx := authenticatedContext(t, testOwner, testEnterprise)
	created, err := harness.client.CreateBase(ctx, &workspacev1.CreateBaseRequest{
		Name: "待删除",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	baseID := created.GetBase().GetId()
	tables, err := harness.structure.TablesByBase(
		context.Background(),
		testEnterprise,
		baseID,
	)
	if err != nil {
		t.Fatalf("TablesByBase() error = %v", err)
	}

	if _, err := harness.client.DeleteBase(ctx, &workspacev1.DeleteBaseRequest{
		BaseId:  baseID,
		Confirm: true,
	}); err != nil {
		t.Fatalf("DeleteBase() error = %v", err)
	}
	if len(harness.recordDeleter.baseIDs) != 1 ||
		harness.recordDeleter.baseIDs[0] != baseID ||
		len(harness.recordDeleter.tableIDs) != len(tables) ||
		harness.recordDeleter.tableIDs[0] != tables[0].ID {
		t.Fatalf("record deletion = bases %v tables %v",
			harness.recordDeleter.baseIDs, harness.recordDeleter.tableIDs)
	}
	if len(harness.comments.baseIDs) != 1 || harness.comments.baseIDs[0] != baseID {
		t.Fatalf("comment deletion = %v", harness.comments.baseIDs)
	}
	if len(harness.links.baseIDs) != 1 || harness.links.baseIDs[0] != baseID {
		t.Fatalf("link cleanup = %v", harness.links.baseIDs)
	}
	if len(harness.outbox.events) != 1 {
		t.Fatalf("outbox events = %d, want 1", len(harness.outbox.events))
	}
	forwarded, err := harness.forwarder.ForwardOnce(context.Background())
	if err != nil {
		t.Fatalf("ForwardOnce() error = %v", err)
	}
	if forwarded != 1 {
		t.Fatalf("ForwardOnce() forwarded = %d, want 1", forwarded)
	}
	deliveryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	delivery, err := harness.auditEvents.Receive(deliveryCtx)
	if err != nil {
		t.Fatalf("Receive(audit.append) error = %v", err)
	}
	event := delivery.Event
	if event.Header.Type != "audit.append" ||
		event.Header.Producer != "workspace" ||
		event.Header.BaseID != baseID {
		t.Fatalf("audit event header = %+v", event.Header)
	}
	var payload struct {
		Action    string         `json:"action"`
		Operator  string         `json:"operator"`
		Target    string         `json:"target"`
		DetailMap map[string]any `json:"detail_map"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if payload.Action != "AUDIT_ACTION_BASE_DELETED" ||
		payload.Operator != testOwner ||
		payload.Target != baseID ||
		payload.DetailMap["enterprise_id"] != testEnterprise {
		t.Fatalf("audit payload = %+v", payload)
	}
	remainingTables, err := harness.structure.TablesByBase(
		context.Background(),
		testEnterprise,
		baseID,
	)
	if err != nil {
		t.Fatalf("TablesByBase(after delete) error = %v", err)
	}
	if len(remainingTables) != 0 {
		t.Fatalf("remaining tables = %+v", remainingTables)
	}
	if _, err := harness.repository.BaseByID(context.Background(), baseID); !errors.Is(err, base.ErrWorkspaceNotFound) {
		t.Fatalf("BaseByID(after delete) error = %v, want not found", err)
	}
}

func TestDeleteBaseRejectsNonOwnerMember(t *testing.T) {
	harness := newWorkspaceHarness(t)
	created, err := harness.client.CreateBase(
		authenticatedContext(t, testOwner, testEnterprise),
		&workspacev1.CreateBaseRequest{Name: "仅所有者可删"},
	)
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	_, err = harness.client.DeleteBase(
		authenticatedContext(t, testMember, testEnterprise),
		&workspacev1.DeleteBaseRequest{
			BaseId:  created.GetBase().GetId(),
			Confirm: true,
		},
	)
	requireErrorCode(t, err, 3001)
	if _, err := harness.repository.BaseByID(
		context.Background(),
		created.GetBase().GetId(),
	); err != nil {
		t.Fatalf("BaseByID() after rejected delete error = %v", err)
	}
}
