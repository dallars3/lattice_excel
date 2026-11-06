package share

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

// 匿名链接探测（H-95）：未登录访客在 /s/{token} 入口没有会话与 Principal，
// 网关只能凭 link_context 里的 token 回源 core 判定链接当前是否仍然有效。
// 只有当前启用、格式合法的链接才 allow=true（进入注册分支）；伪造/已重置/
// 已关闭/导入中的 Base 一律 allow=false，网关据此统一 302 失效页。
// 判定口径必须与 IAM 注册通道的 ResolveLinkToken 一致，避免入口放行、
// 注册拒绝（或相反）的分裂结论。

// anonymousProbeRequest 构造未登录访客的链接探测请求：无 Principal，仅凭 token。
func anonymousProbeRequest(token string) *sharev1.CheckAccessRequest {
	return &sharev1.CheckAccessRequest{
		Action:      sharev1.Action_ACTION_VIEW,
		LinkContext: &commonv1.LinkContext{LinkToken: token},
	}
}

func TestAnonymousLinkProbeAllowsOnlyLiveTokens(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID     = "t13_probe_base_live"
		closedBase = "t13_probe_base_closed"
	)
	seedShareLink(t, harness, baseID, "t13_probe_token_live", "anyone", "viewer", true)
	seedShareLink(t, harness, closedBase, "t13_probe_token_closed", "anyone", "filler", false)

	tests := []struct {
		name      string
		token     string
		wantAllow bool
		wantRole  sharev1.Role
	}{
		{
			name:      "live anyone/viewer token is allowed",
			token:     "t13_probe_token_live",
			wantAllow: true,
			wantRole:  sharev1.Role_ROLE_VIEWER,
		},
		{
			name:      "forged token never seen by core is denied",
			token:     "t13_probe_token_forged",
			wantAllow: false,
		},
		{
			name:      "closed link is denied",
			token:     "t13_probe_token_closed",
			wantAllow: false,
		},
		{
			name:      "blank token is denied",
			token:     "",
			wantAllow: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := harness.client.CheckAccess(ctx, anonymousProbeRequest(test.token))
			if err != nil {
				t.Fatalf("CheckAccess(anonymous probe %q) error = %v", test.token, err)
			}
			if decision.GetAllow() != test.wantAllow {
				t.Fatalf("CheckAccess(anonymous probe %q) allow = %v, want %v",
					test.token, decision.GetAllow(), test.wantAllow)
			}
			if test.wantAllow && decision.GetRole() != test.wantRole {
				t.Fatalf("CheckAccess(anonymous probe %q) role = %v, want %v",
					test.token, decision.GetRole(), test.wantRole)
			}
		})
	}
}

func TestAnonymousLinkProbeDeniesTokenRotatedByReset(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID       = "t13_probe_base_reset"
		enterpriseID = "t13_probe_enterprise_reset"
		managerID    = "t13_probe_manager_reset"
		oldToken     = "t13_probe_token_old"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = managerID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	seedShareCollaborator(t, harness, baseID, managerID, "manager", true)
	seedShareLink(t, harness, baseID, oldToken, "anyone", "viewer", true)
	managerContext := contextWithSharePrincipal(t, principalForUser(managerID, enterpriseID))

	reset, err := harness.client.ResetLink(managerContext, &sharev1.ResetLinkRequest{
		BaseId: baseID,
	})
	if err != nil {
		t.Fatalf("ResetLink error = %v", err)
	}
	newToken := linkTokenOfURL(t, reset.GetLinkConfig().GetUrl())
	if newToken == "" || newToken == oldToken {
		t.Fatalf("ResetLink token = %q, want a fresh token distinct from %q", newToken, oldToken)
	}

	oldDecision, err := harness.client.CheckAccess(ctx, anonymousProbeRequest(oldToken))
	if err != nil {
		t.Fatalf("CheckAccess(anonymous probe old token) error = %v", err)
	}
	if oldDecision.GetAllow() {
		t.Fatal("CheckAccess(anonymous probe old token) allow = true, want false immediately after reset")
	}
	newDecision, err := harness.client.CheckAccess(ctx, anonymousProbeRequest(newToken))
	if err != nil {
		t.Fatalf("CheckAccess(anonymous probe new token) error = %v", err)
	}
	if !newDecision.GetAllow() {
		t.Fatal("CheckAccess(anonymous probe new token) allow = false, want true")
	}
}

func TestAnonymousLinkProbeDeniesTokenClosedByManager(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const (
		baseID       = "t13_probe_base_force"
		enterpriseID = "t13_probe_enterprise_force"
		managerID    = "t13_probe_manager_force"
		token        = "t13_probe_token_force"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = managerID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	seedShareCollaborator(t, harness, baseID, managerID, "manager", true)
	seedShareLink(t, harness, baseID, token, "anyone", "viewer", true)
	managerContext := contextWithSharePrincipal(t, principalForUser(managerID, enterpriseID))

	if _, err := harness.client.CloseLink(managerContext, &sharev1.CloseLinkRequest{
		BaseId: baseID,
	}); err != nil {
		t.Fatalf("CloseLink error = %v", err)
	}

	decision, err := harness.client.CheckAccess(ctx, anonymousProbeRequest(token))
	if err != nil {
		t.Fatalf("CheckAccess(anonymous probe closed token) error = %v", err)
	}
	if decision.GetAllow() {
		t.Fatal("CheckAccess(anonymous probe closed token) allow = true, want false immediately after close")
	}
}

func TestAnonymousLinkProbeDeniesWhileBaseImporting(t *testing.T) {
	harness := newShareTestHarness(t)
	ctx := context.Background()
	const baseID = "t13_probe_base_importing"
	seedShareLink(t, harness, baseID, "t13_probe_token_importing", "anyone", "viewer", true)
	harness.importLock[baseID] = true

	decision, err := harness.client.CheckAccess(ctx, anonymousProbeRequest("t13_probe_token_importing"))
	if err != nil {
		t.Fatalf("CheckAccess(anonymous probe importing) error = %v", err)
	}
	if decision.GetAllow() {
		t.Fatal("CheckAccess(anonymous probe importing) allow = true, want false while base imports")
	}
}

func TestAnonymousLinkProbeStillRejectsPrincipallessNonProbeRequests(t *testing.T) {
	harness := newShareTestHarness(t)

	_, err := harness.client.CheckAccess(context.Background(), &sharev1.CheckAccessRequest{
		Action: sharev1.Action_ACTION_VIEW,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CheckAccess(no principal, no token) error = %v, want 1001", err)
	}
}

// linkTokenOfURL extracts the rotated token from the link URL shape "/s/{token}".
func linkTokenOfURL(t *testing.T, value string) string {
	t.Helper()
	if !strings.HasPrefix(value, "/s/") {
		return ""
	}
	return strings.TrimPrefix(value, "/s/")
}
