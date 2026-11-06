package share

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

// TestListOpenLinksByEnterpriseFillsBaseNameManagerRoleAndOpenedAt 锁 H-71/缺陷
// #16 契约：企业管理员「任何人」外链清单每条记录 base_name（Base 名称）、
// manager（Base 可管理者用户名）、role、opened_at 均非空，管理员据此识别外链
// 归属与责任人；H-72 强关直接复用条目 base_id 寻址。
func TestListOpenLinksByEnterpriseFillsBaseNameManagerRoleAndOpenedAt(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000001401"
		enterpriseID = "01J00000000000000000001402"
		ownerID      = "01J00000000000000000001403"
		adminID      = "01J00000000000000000001404"
		token        = "t14-open-links-token"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseNames[baseID] = "t14-客户数据"
	harness.baseOwner[baseID] = ownerID
	harness.profiles[ownerID] = &iamv1.User{
		Id: ownerID, Username: "t14_owner", DisplayName: "T14 所有者",
	}
	harness.memberRole[enterpriseID+"/"+adminID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	seedShareLink(t, harness, baseID, token, "anyone", "filler", true)

	response, err := harness.client.ListOpenLinksByEnterprise(
		contextWithSharePrincipal(t, principalForUser(adminID, enterpriseID)),
		&sharev1.ListOpenLinksByEnterpriseRequest{Limit: 100},
	)
	if err != nil {
		t.Fatalf("ListOpenLinksByEnterprise() error = %v", err)
	}
	var entry *sharev1.OpenLink
	for _, link := range response.GetLinkList() {
		if link.GetBaseId() == baseID {
			entry = link
			break
		}
	}
	if entry == nil {
		t.Fatalf("open links missing base %s: %+v", baseID, response.GetLinkList())
	}
	if entry.GetBaseName() == "" ||
		entry.GetManager() == "" ||
		entry.GetRole() == sharev1.Role_ROLE_UNSPECIFIED ||
		entry.GetOpenedAt() <= 0 {
		t.Fatalf("open link entry = %+v, want base_name/manager/role/opened_at all non-empty", entry)
	}
	if entry.GetBaseName() != "t14-客户数据" {
		t.Fatalf("base_name = %q, want %q", entry.GetBaseName(), "t14-客户数据")
	}
	if entry.GetManager() != "t14_owner" {
		t.Fatalf("manager = %q, want username %q", entry.GetManager(), "t14_owner")
	}
	if entry.GetRole() != sharev1.Role_ROLE_FILLER {
		t.Fatalf("role = %v, want ROLE_FILLER", entry.GetRole())
	}
}

// TestListOpenLinksByEnterpriseDeniesNonAdmin 锁 H-71 权限边界：非企业管理员
// （普通成员）与无企业归属主体访问清单一律 3001。
func TestListOpenLinksByEnterpriseDeniesNonAdmin(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000001411"
		enterpriseID = "01J00000000000000000001412"
		memberID     = "01J00000000000000000001413"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.memberRole[enterpriseID+"/"+memberID] = iamv1.MemberRole_MEMBER_ROLE_MEMBER
	seedShareLink(t, harness, baseID, "t14-denied-token", "anyone", "viewer", true)

	tests := []struct {
		name      string
		principal *commonv1.Principal
	}{
		{
			name:      "internal member without owner/manager role",
			principal: principalForUser(memberID, enterpriseID),
		},
		{
			name: "principal without enterprise",
			principal: &commonv1.Principal{
				UserId:   memberID,
				Internal: false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := harness.client.ListOpenLinksByEnterprise(
				contextWithSharePrincipal(t, tt.principal),
				&sharev1.ListOpenLinksByEnterpriseRequest{Limit: 100},
			)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("ListOpenLinksByEnterprise(%s) error = %v, want 3001", tt.name, err)
			}
		})
	}
}
