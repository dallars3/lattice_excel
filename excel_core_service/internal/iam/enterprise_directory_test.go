package iam

import (
	"context"
	"testing"

	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
)

// 票 04（N2）：EnterpriseDirectoryProvider——人员目录并集的企业侧只读来源。
// 跑在独立测试库 excel_core_test 上，经真实注册/邀请/接受流程造数：
//  1. active 成员名册含所有者与成员（username/display_name 取真实账号身份）；
//  2. OwnerID 返回企业所有者；
//  3. 成员被移除（DisableMember）后名册不再包含该成员。
func TestEnterpriseDirectoryProviderRosterOwnerAndRemoval(t *testing.T) {
	harness := newEnterpriseTestHarness(t)

	owner := registerTestUser(t, harness.client, "Dir_Owner", "目录企业")
	ownerContext := contextWithPrincipal(t, testPrincipal(owner))
	memberUser := registerTestUser(t, harness.client, "Dir_Member", "目录自建企业")
	memberContext := contextWithPrincipal(t, testPrincipal(memberUser))
	invite := inviteTestUser(t, harness.client, ownerContext, "Dir_Member")
	if _, err := harness.client.AcceptInvite(memberContext, &iamv1.AcceptInviteRequest{
		InviteId: invite.GetId(),
	}); err != nil {
		t.Fatalf("AcceptInvite() error = %v", err)
	}

	provider := NewEnterpriseDirectoryProvider(base.NewIAMRepository(harness.db))
	enterpriseID := owner.GetEnterpriseId()

	members, err := provider.MembersByEnterprise(context.Background(), enterpriseID)
	if err != nil {
		t.Fatalf("MembersByEnterprise() error = %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("MembersByEnterprise() count = %d, want 2 (owner+member)", len(members))
	}
	ids := map[string]base.IAMMember{}
	for _, member := range members {
		ids[member.UserID] = member
	}
	if _, ok := ids[owner.GetId()]; !ok {
		t.Fatalf("roster missing owner %q: %+v", owner.GetId(), members)
	}
	if _, ok := ids[memberUser.GetId()]; !ok {
		t.Fatalf("roster missing member %q: %+v", memberUser.GetId(), members)
	}
	if ids[owner.GetId()].Username != "Dir_Owner" || ids[owner.GetId()].DisplayName != "Dir_Owner" {
		t.Fatalf("owner roster identity = %q/%q, want Dir_Owner/Dir_Owner",
			ids[owner.GetId()].Username, ids[owner.GetId()].DisplayName)
	}
	if ids[memberUser.GetId()].Username != "Dir_Member" || ids[memberUser.GetId()].DisplayName != "Dir_Member" {
		t.Fatalf("member roster identity = %q/%q, want Dir_Member/Dir_Member",
			ids[memberUser.GetId()].Username, ids[memberUser.GetId()].DisplayName)
	}

	ownerID, err := provider.OwnerID(context.Background(), enterpriseID)
	if err != nil {
		t.Fatalf("OwnerID() error = %v", err)
	}
	if ownerID != owner.GetId() {
		t.Fatalf("OwnerID() = %q, want %q", ownerID, owner.GetId())
	}

	// 成员被移除后：目录不再包含该成员。
	if _, err := harness.client.DisableMember(ownerContext, &iamv1.DisableMemberRequest{
		UserId: memberUser.GetId(),
	}); err != nil {
		t.Fatalf("DisableMember() error = %v", err)
	}
	members, err = provider.MembersByEnterprise(context.Background(), enterpriseID)
	if err != nil {
		t.Fatalf("MembersByEnterprise(after disable) error = %v", err)
	}
	if len(members) != 1 || members[0].UserID != owner.GetId() {
		t.Fatalf("roster after disable = %+v, want owner only", members)
	}
}

// 未装配仓储（nil 接收者）必须显式报错，不得静默返回空名册。
func TestEnterpriseDirectoryProviderRequiresRepository(t *testing.T) {
	provider := NewEnterpriseDirectoryProvider(nil)
	if _, err := provider.MembersByEnterprise(context.Background(), "ent"); err == nil {
		t.Fatal("MembersByEnterprise(nil repository) error = nil, want error")
	}
}
