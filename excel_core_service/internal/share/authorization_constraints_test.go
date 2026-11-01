package share

import (
	"context"
	"encoding/base64"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

func TestExternalAndAnyoneAuthorizationCapsCannotBeBypassed(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID       = "01J00000000000000000003201"
		enterpriseID = "01J00000000000000000003202"
		managerID    = "01J00000000000000000003203"
		externalID   = "01J00000000000000000003204"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	seedShareCollaborator(t, harness, baseID, managerID, "manager", true)
	seedShareCollaborator(t, harness, baseID, externalID, "manager", false)

	resolved, err := harness.client.ResolveRole(ctx, &sharev1.ResolveRoleRequest{
		Principal: &commonv1.Principal{UserId: externalID, Internal: false},
		BaseId:    baseID,
	})
	if err != nil {
		t.Fatalf("ResolveRole(external manager row) error = %v", err)
	}
	if resolved.GetRole() != sharev1.Role_ROLE_EDITOR {
		t.Fatalf("ResolveRole(external manager row) role = %v, want ROLE_EDITOR", resolved.GetRole())
	}

	export, err := harness.client.CheckAccess(ctx, &sharev1.CheckAccessRequest{
		Principal: &commonv1.Principal{UserId: externalID, Internal: false},
		BaseId:    baseID,
		Action:    sharev1.Action_ACTION_EXPORT,
	})
	if err != nil {
		t.Fatalf("CheckAccess(external export) error = %v", err)
	}
	if export.GetAllow() {
		t.Fatal("CheckAccess(external export) allow = true, want false")
	}

	managerContext := contextWithSharePrincipal(t, &commonv1.Principal{
		UserId:       managerID,
		EnterpriseId: enterpriseID,
		Internal:     true,
	})
	_, err = harness.client.UpdateLink(managerContext, &sharev1.UpdateLinkRequest{
		BaseId: baseID,
		Scope:  sharev1.LinkScope_LINK_SCOPE_ANYONE,
		Role:   sharev1.Role_ROLE_EDITOR,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("UpdateLink(anyone/editor) error = %v, want 1001", err)
	}
}

func contextWithSharePrincipal(t *testing.T, principal *commonv1.Principal) context.Context {
	t.Helper()
	payload, err := proto.Marshal(principal)
	if err != nil {
		t.Fatalf("marshal principal: %v", err)
	}
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"x-principal",
		base64.StdEncoding.EncodeToString(payload),
	)
}
