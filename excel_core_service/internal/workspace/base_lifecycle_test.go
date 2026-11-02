package workspace_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	workspacev1 "excel/api/workspace/v1"
)

func TestRenameSearchAndTransferBase(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	memberCtx := authenticatedContext(t, testMember, testEnterprise)

	created, err := harness.client.CreateBase(ownerCtx, &workspacev1.CreateBaseRequest{
		Name: "年度计划",
	})
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	baseID := created.GetBase().GetId()
	if _, err := harness.client.ToggleStar(ownerCtx, &workspacev1.ToggleStarRequest{
		BaseId:  baseID,
		Starred: true,
	}); err != nil {
		t.Fatalf("ToggleStar() error = %v", err)
	}
	if _, err := harness.client.ToggleStar(ownerCtx, &workspacev1.ToggleStarRequest{
		BaseId:  baseID,
		Starred: true,
	}); err != nil {
		t.Fatalf("repeated ToggleStar() error = %v", err)
	}

	renamed, err := harness.client.RenameBase(ownerCtx, &workspacev1.RenameBaseRequest{
		BaseId: baseID,
		Name:   "  预算计划  ",
	})
	if err != nil {
		t.Fatalf("RenameBase() error = %v", err)
	}
	if renamed.GetBase().GetName() != "预算计划" {
		t.Fatalf("renamed Base = %q", renamed.GetBase().GetName())
	}
	search, err := harness.client.SearchBases(ownerCtx, &workspacev1.SearchBasesRequest{
		Q: "预算",
	})
	if err != nil {
		t.Fatalf("SearchBases() error = %v", err)
	}
	if len(search.GetBaseList()) != 1 || search.GetBaseList()[0].GetId() != baseID {
		t.Fatalf("search results = %+v", search.GetBaseList())
	}
	memberSearch, err := harness.client.SearchBases(memberCtx, &workspacev1.SearchBasesRequest{
		Q: "预算",
	})
	if err != nil {
		t.Fatalf("member SearchBases() error = %v", err)
	}
	if len(memberSearch.GetBaseList()) != 0 {
		t.Fatalf("member search leaked inaccessible Base: %+v", memberSearch.GetBaseList())
	}

	if _, err := harness.client.TransferBase(memberCtx, &workspacev1.TransferBaseRequest{
		BaseId:     baseID,
		NewOwnerId: testManager,
	}); err == nil {
		t.Fatal("TransferBase(member) error = nil, want permission denied")
	} else {
		requireErrorCode(t, err, 3001)
	}
	if _, err := harness.client.TransferBase(ownerCtx, &workspacev1.TransferBaseRequest{
		BaseId:     baseID,
		NewOwnerId: testMember,
	}); err != nil {
		t.Fatalf("TransferBase(owner) error = %v", err)
	}
	transferred, err := harness.repository.BaseByID(context.Background(), baseID)
	if err != nil {
		t.Fatalf("BaseByID() error = %v", err)
	}
	if transferred.OwnerID != testMember {
		t.Fatalf("new owner = %q, want %q", transferred.OwnerID, testMember)
	}
	if countRows(t, harness, `
		SELECT COUNT(*) FROM excel_workspace_stars
		WHERE base_id = ? AND user_id = ?
	`, baseID, testOwner) != 1 {
		t.Fatal("TransferBase() changed the previous owner's star")
	}
	if countRows(t, harness, `
		SELECT COUNT(*) FROM excel_workspace_recent
		WHERE base_id = ? AND user_id = ?
	`, baseID, testOwner) != 1 {
		t.Fatal("TransferBase() changed the previous owner's recent access")
	}
	if len(harness.outbox.events) != 1 {
		t.Fatalf("transfer audit events = %d, want 1", len(harness.outbox.events))
	}
	if forwarded, err := harness.forwarder.ForwardOnce(context.Background()); err != nil ||
		forwarded != 1 {
		t.Fatalf("ForwardOnce() = %d, %v, want 1, nil", forwarded, err)
	}
	deliveryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	delivery, err := harness.auditEvents.Receive(deliveryCtx)
	if err != nil {
		t.Fatalf("Receive(audit.append) error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(delivery.Event.Payload, &payload); err != nil {
		t.Fatalf("decode transfer audit payload: %v", err)
	}
	if payload["action"] != "AUDIT_ACTION_BASE_OWNERSHIP_TRANSFERRED" {
		t.Fatalf("transfer audit action = %v", payload["action"])
	}
}

func countRows(t *testing.T, harness *workspaceHarness, query string, args ...any) int {
	t.Helper()
	var count int
	if err := harness.db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}
