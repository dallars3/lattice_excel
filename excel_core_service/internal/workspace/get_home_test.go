package workspace_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	iamv1 "excel/api/iam/v1"
	workspacev1 "excel/api/workspace/v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
)

func TestHomeReturnsFourGroupsAndPendingInvitations(t *testing.T) {
	harness := newWorkspaceHarness(t)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	managerCtx := authenticatedContext(t, testManager, testEnterprise)

	older, err := harness.client.CreateBase(ownerCtx, &workspacev1.CreateBaseRequest{
		Name: "较早访问",
	})
	if err != nil {
		t.Fatalf("CreateBase(older) error = %v", err)
	}
	newer, err := harness.client.CreateBase(ownerCtx, &workspacev1.CreateBaseRequest{
		Name: "最近访问",
	})
	if err != nil {
		t.Fatalf("CreateBase(newer) error = %v", err)
	}
	shared, err := harness.client.CreateBase(managerCtx, &workspacev1.CreateBaseRequest{
		Name: "共享给我",
	})
	if err != nil {
		t.Fatalf("CreateBase(shared) error = %v", err)
	}

	if _, err := harness.db.ExecContext(
		context.Background(),
		`INSERT INTO excel_share_collaborators
			(base_id, user_id, role, internal, source, granted_at)
		 VALUES (?, ?, 'editor', 1, 'invite', ?)`,
		shared.GetBase().GetId(),
		testOwner,
		harness.now.UnixMilli(),
	); err != nil {
		t.Fatalf("insert shared collaborator: %v", err)
	}
	if err := harness.repository.RecordAccess(
		context.Background(),
		testOwner,
		older.GetBase().GetId(),
		harness.now.Add(-2*time.Hour),
	); err != nil {
		t.Fatalf("RecordAccess(older) error = %v", err)
	}
	if err := harness.repository.RecordAccess(
		context.Background(),
		testOwner,
		shared.GetBase().GetId(),
		harness.now.Add(1*time.Hour),
	); err != nil {
		t.Fatalf("RecordAccess(shared) error = %v", err)
	}
	if _, err := harness.client.ToggleStar(ownerCtx, &workspacev1.ToggleStarRequest{
		BaseId:  newer.GetBase().GetId(),
		Starred: true,
	}); err != nil {
		t.Fatalf("ToggleStar() error = %v", err)
	}
	harness.invitations.invitations = []*iamv1.Invitation{
		{Id: "invite-1", Status: iamv1.InvitationStatus_INVITATION_STATUS_PENDING},
		{Id: "invite-2", Status: iamv1.InvitationStatus_INVITATION_STATUS_PENDING},
	}

	result, err := harness.workspaceServer.LoadHome(
		authenticatedIncomingContext(t, testOwner, testEnterprise),
	)
	if err != nil {
		t.Fatalf("LoadHome() error = %v", err)
	}
	if result.IsPartial {
		t.Fatalf("LoadHome() partial = %v, want false", result.PartialGroups)
	}
	home := result.Home
	if len(home.GetRecentList()) != 3 {
		t.Fatalf("recent count = %d, want 3", len(home.GetRecentList()))
	}
	if home.GetRecentList()[0].GetId() != shared.GetBase().GetId() {
		t.Fatalf("first recent = %q, want shared Base", home.GetRecentList()[0].GetName())
	}
	if len(home.GetCreatedList()) != 2 {
		t.Fatalf("created count = %d, want 2", len(home.GetCreatedList()))
	}
	if len(home.GetSharedList()) != 1 ||
		home.GetSharedList()[0].GetId() != shared.GetBase().GetId() ||
		home.GetSharedList()[0].GetMyRole() != "editor" {
		t.Fatalf("shared list = %+v", home.GetSharedList())
	}
	if len(home.GetStarList()) != 1 ||
		home.GetStarList()[0].GetId() != newer.GetBase().GetId() {
		t.Fatalf("star list = %+v", home.GetStarList())
	}
	if home.GetPendingInvitationCount() != 2 {
		t.Fatalf("pending invitation count = %d, want 2", home.GetPendingInvitationCount())
	}
}

func TestHomeProviderFailureIsPartialWithoutError(t *testing.T) {
	harness := newWorkspaceHarness(t)
	if _, err := harness.client.CreateBase(
		authenticatedContext(t, testOwner, testEnterprise),
		&workspacev1.CreateBaseRequest{Name: "仍应返回"},
	); err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	harness.invitations.err = errors.New("IAM unavailable")

	result, err := harness.workspaceServer.LoadHome(
		authenticatedIncomingContext(t, testOwner, testEnterprise),
	)
	if err != nil {
		t.Fatalf("LoadHome() error = %v, want partial success", err)
	}
	if !result.IsPartial {
		t.Fatal("LoadHome() IsPartial = false, want true")
	}
	if result.Home.GetPendingInvitationCount() != 0 {
		t.Fatalf("pending invitation count = %d, want empty group", result.Home.GetPendingInvitationCount())
	}
	if len(result.Home.GetCreatedList()) != 1 {
		t.Fatalf("created count = %d, want unaffected group", len(result.Home.GetCreatedList()))
	}
}

func authenticatedIncomingContext(
	t *testing.T,
	userID, enterpriseID string,
) context.Context {
	t.Helper()
	payload, err := proto.Marshal(&commonv1.Principal{
		UserId:       userID,
		EnterpriseId: enterpriseID,
		Internal:     enterpriseID != "",
	})
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-principal", base64.StdEncoding.EncodeToString(payload)),
	)
}
