package share

import (
	"context"
	"testing"
	"time"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

func TestResolveRoleMergesInternalExternalAndAnyoneIdentitiesFromCurrentGrants(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID          = "01J00000000000000000003101"
		anyoneBaseID    = "01J00000000000000000003102"
		enterpriseID    = "01J00000000000000000003103"
		internalID      = "01J00000000000000000003104"
		externalID      = "01J00000000000000000003105"
		anyoneID        = "01J00000000000000000003106"
		enterpriseToken = "current-enterprise-token"
		anyoneToken     = "current-anyone-token"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseResolver[anyoneBaseID] = enterpriseID
	harness.memberRole[enterpriseID+"/"+internalID] = iamv1.MemberRole_MEMBER_ROLE_MEMBER
	seedShareCollaborator(t, harness, baseID, internalID, "viewer", true)
	seedShareCollaborator(t, harness, baseID, externalID, "editor", false)
	seedShareLink(t, harness, baseID, enterpriseToken, "enterprise", "filler", true)
	seedShareLink(t, harness, anyoneBaseID, anyoneToken, "anyone", "filler", true)

	tests := []struct {
		name         string
		baseID       string
		principal    *commonv1.Principal
		linkContext  *commonv1.LinkContext
		wantRole     sharev1.Role
		wantInternal bool
		wantRowScope sharev1.RowScope
	}{
		{
			name:   "internal member merges explicit and enterprise link",
			baseID: baseID,
			principal: &commonv1.Principal{
				UserId:       internalID,
				EnterpriseId: enterpriseID,
				Internal:     true,
			},
			linkContext: &commonv1.LinkContext{
				BaseId:    baseID,
				LinkToken: enterpriseToken,
				Role:      "viewer",
			},
			wantRole:     sharev1.Role_ROLE_FILLER,
			wantInternal: true,
			wantRowScope: sharev1.RowScope_ROW_SCOPE_OWN,
		},
		{
			name:   "external explicit collaborator is capped below manager",
			baseID: baseID,
			principal: &commonv1.Principal{
				UserId:   externalID,
				Internal: false,
			},
			wantRole:     sharev1.Role_ROLE_EDITOR,
			wantInternal: false,
			wantRowScope: sharev1.RowScope_ROW_SCOPE_ALL,
		},
		{
			name:   "anyone link uses current stored role instead of session role",
			baseID: anyoneBaseID,
			principal: &commonv1.Principal{
				UserId:   anyoneID,
				Internal: false,
			},
			linkContext: &commonv1.LinkContext{
				BaseId:    anyoneBaseID,
				LinkToken: anyoneToken,
				Role:      "viewer",
			},
			wantRole:     sharev1.Role_ROLE_FILLER,
			wantInternal: false,
			wantRowScope: sharev1.RowScope_ROW_SCOPE_OWN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := harness.client.ResolveRole(ctx, &sharev1.ResolveRoleRequest{
				Principal:   tt.principal,
				BaseId:      tt.baseID,
				LinkContext: tt.linkContext,
			})
			if err != nil {
				t.Fatalf("ResolveRole() error = %v", err)
			}
			if response.GetRole() != tt.wantRole ||
				response.GetInternal() != tt.wantInternal ||
				response.GetRowScope() != tt.wantRowScope {
				t.Fatalf(
					"ResolveRole() = {role:%v internal:%v row_scope:%v}, want {role:%v internal:%v row_scope:%v}",
					response.GetRole(),
					response.GetInternal(),
					response.GetRowScope(),
					tt.wantRole,
					tt.wantInternal,
					tt.wantRowScope,
				)
			}
		})
	}
}

func seedShareCollaborator(
	t *testing.T,
	harness *shareTestHarness,
	baseID, userID, role string,
	internal bool,
) {
	t.Helper()
	if _, err := harness.db.ExecContext(context.Background(), `
		INSERT INTO excel_share_collaborators
			(base_id, user_id, role, internal, source, granted_at)
		VALUES (?, ?, ?, ?, 'invite', ?)
	`, baseID, userID, role, internal, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed collaborator: %v", err)
	}
}

func seedShareLink(
	t *testing.T,
	harness *shareTestHarness,
	baseID, token, scope, role string,
	enabled bool,
) {
	t.Helper()
	if _, err := harness.db.ExecContext(context.Background(), `
		INSERT INTO excel_share_links
			(base_id, token, scope, role, enabled, url, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, baseID, token, scope, role, enabled, "/s/"+token, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed link: %v", err)
	}
}
