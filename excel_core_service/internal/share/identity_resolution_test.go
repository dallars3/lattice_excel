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

// 票 03（N7 收尾）：企业内鉴权按成员行判定——主体当前企业字段与 Base 所属企业
// 解耦。成员行是内部性的唯一事实源：有 Base 所属企业的有效成员行即内部身份
// （导出放行、协作者角色不封顶），无成员行一律外部（导出拒绝、角色封顶）。
func TestResolveRoleClassifiesInternalByBaseEnterpriseMembership(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID         = "01J00000000000000000003111"
		baseEnterprise = "01J00000000000000000003112"
		memberID       = "01J00000000000000000003113"
		memberHome     = "01J00000000000000000003114"
		externalID     = "01J00000000000000000003115"
	)
	harness.baseResolver[baseID] = baseEnterprise
	harness.memberRole[baseEnterprise+"/"+memberID] = iamv1.MemberRole_MEMBER_ROLE_MEMBER
	seedShareCollaborator(t, harness, baseID, memberID, "editor", false)
	seedShareCollaborator(t, harness, baseID, externalID, "editor", false)

	tests := []struct {
		name         string
		principal    *commonv1.Principal
		wantRole     sharev1.Role
		wantInternal bool
		wantExport   bool
	}{
		{
			name: "member row in base enterprise classifies internal despite foreign principal enterprise",
			principal: &commonv1.Principal{
				UserId:       memberID,
				EnterpriseId: memberHome,
				Internal:     true,
			},
			wantRole:     sharev1.Role_ROLE_EDITOR,
			wantInternal: true,
			wantExport:   true,
		},
		{
			name: "collaborator without member row stays external and export denied",
			principal: &commonv1.Principal{
				UserId:   externalID,
				Internal: false,
			},
			wantRole:     sharev1.Role_ROLE_EDITOR,
			wantInternal: false,
			wantExport:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := harness.client.ResolveRole(ctx, &sharev1.ResolveRoleRequest{
				Principal: tt.principal,
				BaseId:    baseID,
			})
			if err != nil {
				t.Fatalf("ResolveRole() error = %v", err)
			}
			if response.GetRole() != tt.wantRole || response.GetInternal() != tt.wantInternal {
				t.Fatalf(
					"ResolveRole() = {role:%v internal:%v}, want {role:%v internal:%v}",
					response.GetRole(),
					response.GetInternal(),
					tt.wantRole,
					tt.wantInternal,
				)
			}
			export, err := harness.client.CheckAccess(ctx, &sharev1.CheckAccessRequest{
				Principal: tt.principal,
				BaseId:    baseID,
				Action:    sharev1.Action_ACTION_EXPORT,
			})
			if err != nil {
				t.Fatalf("CheckAccess(export) error = %v", err)
			}
			if export.GetAllow() != tt.wantExport {
				t.Fatalf("CheckAccess(export) allow = %v, want %v", export.GetAllow(), tt.wantExport)
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
