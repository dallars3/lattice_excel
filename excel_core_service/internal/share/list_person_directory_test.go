package share

import (
	"context"
	"sort"
	"testing"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
	"excel_core_service/base"
)

// 票 04（N2）：人员字段候选目录升级为「Base 外部协作者 ∪ 企业成员 ∪ 所有者」并集，
// 服务端按调用者身份裁剪。四种视角集成用例（跑在独立测试库 excel_core_test 上，
// 企业侧名册经 EnterpriseDirectoryProvider seam 注入）：
//  A. owner-only 企业，内部所有者调用 → 目录含所有者本人（user_id/username/display_name 形状）；
//  B. 有企业成员，成员同时是协作者/所有者 → 并集去重（同一人不重复）；
//  C. Base 另有外部协作者，内部成员调用 → 协作者 ∪ 企业成员并集；
//  D. 外部协作者调用 → 仅该 Base 协作者集合，不含企业全员名单。

// shareEnterpriseRoster is one enterprise's directory roster in the fake provider.
type shareEnterpriseRoster struct {
	members []base.IAMMember
	ownerID string
}

// shareEnterpriseDirectory fakes the IAM enterprise-directory seam（票 04/N2）。
type shareEnterpriseDirectory map[string]*shareEnterpriseRoster

func (fake shareEnterpriseDirectory) MembersByEnterprise(
	_ context.Context,
	enterpriseID string,
) ([]base.IAMMember, error) {
	roster, ok := fake[enterpriseID]
	if !ok {
		return []base.IAMMember{}, nil
	}
	return roster.members, nil
}

func (fake shareEnterpriseDirectory) OwnerID(
	_ context.Context,
	enterpriseID string,
) (string, error) {
	roster, ok := fake[enterpriseID]
	if !ok {
		return "", nil
	}
	return roster.ownerID, nil
}

func rosterMember(enterpriseID, userID, role string) base.IAMMember {
	return base.IAMMember{
		EnterpriseID: enterpriseID,
		UserID:       userID,
		Username:     userID,
		Role:         role,
		Status:       "active",
	}
}

// personDirectoryIDs extracts the sorted user_id set of a directory response.
func personDirectoryIDs(t *testing.T, response *sharev1.ListPersonDirectoryResponse) []string {
	t.Helper()
	ids := make([]string, 0, len(response.GetCollaboratorList()))
	for _, entry := range response.GetCollaboratorList() {
		ids = append(ids, entry.GetUserId())
	}
	sort.Strings(ids)
	return ids
}

func assertIDSet(t *testing.T, got []string, want ...string) {
	t.Helper()
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if len(got) != len(sorted) {
		t.Fatalf("directory ids = %v, want %v", got, sorted)
	}
	for index := range got {
		if got[index] != sorted[index] {
			t.Fatalf("directory ids = %v, want %v", got, sorted)
		}
	}
}

// 视角 A：owner-only 企业——所有者是唯一内部成员，目录必须含所有者本人，
// 条目形状与协作者列表对齐（id/user_id/username/display_name 恒携带真实身份，
// 目录不携带授权元数据：role/source/granted_at 零值）。
func TestPersonDirectoryOwnerOnlyEnterpriseContainsOwner(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000004201"
		enterpriseID = "01J00000000000000000004202"
		ownerID      = "01J00000000000000000004203"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = ownerID
	harness.memberRole[enterpriseID+"/"+ownerID] = iamv1.MemberRole_MEMBER_ROLE_OWNER
	harness.enterpriseDirectory[enterpriseID] = &shareEnterpriseRoster{
		members: []base.IAMMember{rosterMember(enterpriseID, ownerID, "owner")},
		ownerID: ownerID,
	}
	harness.profiles[ownerID] = &iamv1.User{
		Id: ownerID, Username: "owner", DisplayName: "所有者",
	}
	ownerContext := contextWithSharePrincipal(t, principalForUser(ownerID, enterpriseID))

	response, err := harness.client.ListPersonDirectory(
		ownerContext,
		&sharev1.ListPersonDirectoryRequest{BaseId: baseID},
	)
	if err != nil {
		t.Fatalf("ListPersonDirectory(owner) error = %v", err)
	}
	entries := response.GetCollaboratorList()
	if len(entries) != 1 {
		t.Fatalf("owner-only directory entries = %d (%v), want 1", len(entries), personDirectoryIDs(t, response))
	}
	entry := entries[0]
	if entry.GetId() != ownerID || entry.GetUserId() != ownerID {
		t.Fatalf("owner entry id/user_id = %q/%q, want %q", entry.GetId(), entry.GetUserId(), ownerID)
	}
	if entry.GetUsername() != "owner" || entry.GetDisplayName() != "所有者" {
		t.Fatalf("owner entry username/display_name = %q/%q, want owner/所有者",
			entry.GetUsername(), entry.GetDisplayName())
	}
	if !entry.GetInternal() {
		t.Fatal("owner entry internal = false, want true")
	}
	if entry.GetRole() != sharev1.Role_ROLE_UNSPECIFIED || entry.GetSource() != "" || entry.GetGrantedAt() != 0 {
		t.Fatalf("owner entry carries grant metadata: %+v", entry)
	}
}

// 视角 B：有企业成员的企业——同一人既是所有者又是成员、成员同时被授为 Base
// 协作者时并集必须去重（每人一个条目）。
func TestPersonDirectoryUnionDeduplicatesOwnerMemberCollaborator(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000004211"
		enterpriseID = "01J00000000000000000004212"
		ownerID      = "01J00000000000000000004213"
		memberID     = "01J00000000000000000004214"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = ownerID
	harness.memberRole[enterpriseID+"/"+ownerID] = iamv1.MemberRole_MEMBER_ROLE_OWNER
	harness.enterpriseDirectory[enterpriseID] = &shareEnterpriseRoster{
		members: []base.IAMMember{
			rosterMember(enterpriseID, ownerID, "owner"),
			rosterMember(enterpriseID, memberID, "member"),
		},
		ownerID: ownerID,
	}
	seedShareCollaborator(t, harness, baseID, memberID, "editor", true)
	harness.profiles[ownerID] = &iamv1.User{
		Id: ownerID, Username: "owner", DisplayName: "所有者",
	}
	harness.profiles[memberID] = &iamv1.User{
		Id: memberID, Username: "ed2", DisplayName: "填报员二号",
	}
	ownerContext := contextWithSharePrincipal(t, principalForUser(ownerID, enterpriseID))

	response, err := harness.client.ListPersonDirectory(
		ownerContext,
		&sharev1.ListPersonDirectoryRequest{BaseId: baseID},
	)
	if err != nil {
		t.Fatalf("ListPersonDirectory(owner) error = %v", err)
	}
	assertIDSet(t, personDirectoryIDs(t, response), ownerID, memberID)
	for _, entry := range response.GetCollaboratorList() {
		switch entry.GetUserId() {
		case ownerID:
			if entry.GetUsername() != "owner" || entry.GetDisplayName() != "所有者" {
				t.Fatalf("owner entry identity = %q/%q", entry.GetUsername(), entry.GetDisplayName())
			}
		case memberID:
			if entry.GetUsername() != "ed2" || entry.GetDisplayName() != "填报员二号" {
				t.Fatalf("member entry identity = %q/%q", entry.GetUsername(), entry.GetDisplayName())
			}
		default:
			t.Fatalf("unexpected entry %q", entry.GetUserId())
		}
	}
}

// 视角 C：Base 另有外部协作者——内部成员（非管理者，filler 授权即可调用）目录为
// 「协作者 ∪ 企业成员 ∪ 所有者」并集，外部条目 internal=false。
func TestPersonDirectoryInternalMemberSeesCollaboratorMemberUnion(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000004221"
		enterpriseID = "01J00000000000000000004222"
		ownerID      = "01J00000000000000000004223"
		memberID     = "01J00000000000000000004224"
		externalID   = "01J00000000000000000004225"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = ownerID
	harness.memberRole[enterpriseID+"/"+memberID] = iamv1.MemberRole_MEMBER_ROLE_MEMBER
	harness.enterpriseDirectory[enterpriseID] = &shareEnterpriseRoster{
		members: []base.IAMMember{
			rosterMember(enterpriseID, ownerID, "owner"),
			rosterMember(enterpriseID, memberID, "member"),
		},
		ownerID: ownerID,
	}
	seedShareCollaborator(t, harness, baseID, memberID, "filler", true)
	seedShareCollaborator(t, harness, baseID, externalID, "filler", false)
	harness.profiles[ownerID] = &iamv1.User{
		Id: ownerID, Username: "owner", DisplayName: "所有者",
	}
	harness.profiles[memberID] = &iamv1.User{
		Id: memberID, Username: "ed2", DisplayName: "填报员二号",
	}
	harness.profiles[externalID] = &iamv1.User{
		Id: externalID, Username: "ext_filler", DisplayName: "外部填报",
	}
	memberContext := contextWithSharePrincipal(t, principalForUser(memberID, enterpriseID))

	response, err := harness.client.ListPersonDirectory(
		memberContext,
		&sharev1.ListPersonDirectoryRequest{BaseId: baseID},
	)
	if err != nil {
		t.Fatalf("ListPersonDirectory(internal filler) error = %v", err)
	}
	assertIDSet(t, personDirectoryIDs(t, response), ownerID, memberID, externalID)
	for _, entry := range response.GetCollaboratorList() {
		if entry.GetUserId() == externalID && entry.GetInternal() {
			t.Fatalf("external entry internal = true: %+v", entry)
		}
		if entry.GetUserId() == ownerID && !entry.GetInternal() {
			t.Fatalf("owner entry internal = false: %+v", entry)
		}
	}
}

// 视角 D：外部协作者调用——目录仅含该 Base 协作者集合，企业全员名单不得外泄。
func TestPersonDirectoryExternalCollaboratorSeesOnlyBaseCollaborators(t *testing.T) {
	harness := newShareTestHarness(t)
	const (
		baseID       = "01J00000000000000000004231"
		enterpriseID = "01J00000000000000000004232"
		ownerID      = "01J00000000000000000004233"
		memberID     = "01J00000000000000000004234"
		externalID   = "01J00000000000000000004235"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = ownerID
	harness.memberRole[enterpriseID+"/"+ownerID] = iamv1.MemberRole_MEMBER_ROLE_OWNER
	harness.enterpriseDirectory[enterpriseID] = &shareEnterpriseRoster{
		members: []base.IAMMember{
			rosterMember(enterpriseID, ownerID, "owner"),
			rosterMember(enterpriseID, memberID, "member"),
		},
		ownerID: ownerID,
	}
	seedShareCollaborator(t, harness, baseID, externalID, "viewer", false)
	harness.profiles[ownerID] = &iamv1.User{
		Id: ownerID, Username: "owner", DisplayName: "所有者",
	}
	harness.profiles[memberID] = &iamv1.User{
		Id: memberID, Username: "ed2", DisplayName: "填报员二号",
	}
	harness.profiles[externalID] = &iamv1.User{
		Id: externalID, Username: "ext_viewer", DisplayName: "外部查看",
	}
	// 外部协作者主体：无企业归属（注册即外部账号），Internal=false。
	externalContext := contextWithSharePrincipal(t, &commonv1.Principal{UserId: externalID})

	response, err := harness.client.ListPersonDirectory(
		externalContext,
		&sharev1.ListPersonDirectoryRequest{BaseId: baseID},
	)
	if err != nil {
		t.Fatalf("ListPersonDirectory(external viewer) error = %v", err)
	}
	assertIDSet(t, personDirectoryIDs(t, response), externalID)
	for _, entry := range response.GetCollaboratorList() {
		if entry.GetUserId() == externalID &&
			(entry.GetUsername() != "ext_viewer" || entry.GetDisplayName() != "外部查看") {
			t.Fatalf("external entry identity = %q/%q", entry.GetUsername(), entry.GetDisplayName())
		}
	}
}
