package share

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "excel/api/iam/v1"
	sharev1 "excel/api/share/v1"
)

// TestCollaboratorGrantFillerImmediateWriteAndEntryShape 锁 P0 冒烟三条契约：
//  1. H-64 接受 role=filler（内部与外部均可授；外部 manager 仍拒）；授权后立即
//     CheckAllow(create_record/edit_own) 为 allow 且行级范围 OWN——新协作者加入后
//     1 分钟内可完成一次写（无 9001 传播窗口）。
//  2. H-63 条目携带稳定 id 且 username/display_name 恒为真实值（外部条目不得落 user_id）。
//  3. H-65/H-66 以条目 id（=user_id）寻址成功，用户名寻址兜底成功，未知名 1001。
func TestCollaboratorGrantFillerImmediateWriteAndEntryShape(t *testing.T) {
	harness := newShareTestHarnessWithPolicy(t, false)
	ctx := context.Background()
	const (
		baseID       = "01J00000000000000000003701"
		enterpriseID = "01J00000000000000000003702"
		managerID    = "01J00000000000000000003703"
		internalID   = "01J00000000000000000003704"
		externalID   = "01J00000000000000000003705"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = managerID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	harness.memberRole[enterpriseID+"/"+internalID] = iamv1.MemberRole_MEMBER_ROLE_MEMBER
	harness.userLookup["ed2"] = shareUserSpec{userID: internalID, internal: true}
	harness.userLookup["ext_filler"] = shareUserSpec{userID: externalID, internal: false}
	harness.userLookup["manager_name"] = shareUserSpec{userID: managerID, internal: true}
	seedShareCollaborator(t, harness, baseID, managerID, "manager", true)
	harness.profiles[managerID] = &iamv1.User{
		Id: managerID, Username: "manager_name", DisplayName: "所有者",
	}
	harness.profiles[internalID] = &iamv1.User{
		Id: internalID, Username: "ed2", DisplayName: "填报员二号",
	}
	harness.profiles[externalID] = &iamv1.User{
		Id: externalID, Username: "ext_filler", DisplayName: "外部填报",
	}
	managerContext := contextWithSharePrincipal(t, principalForUser(managerID, enterpriseID))

	// H-64：内部成员授 filler 成功，出参为真实条目形状。
	added, err := harness.client.AddCollaborator(managerContext, &sharev1.AddCollaboratorRequest{
		BaseId:   baseID,
		Username: "ed2",
		Role:     sharev1.Role_ROLE_FILLER,
	})
	if err != nil {
		t.Fatalf("AddCollaborator(internal filler) error = %v", err)
	}
	entry := added.GetCollaborator()
	if entry.GetId() != internalID || entry.GetUserId() != internalID {
		t.Fatalf("AddCollaborator entry id/user_id = %q/%q, want %q", entry.GetId(), entry.GetUserId(), internalID)
	}
	if entry.GetUsername() != "ed2" || entry.GetDisplayName() != "填报员二号" {
		t.Fatalf(
			"AddCollaborator entry username/display_name = %q/%q, want real identity",
			entry.GetUsername(),
			entry.GetDisplayName(),
		)
	}
	if entry.GetRole() != sharev1.Role_ROLE_FILLER || !entry.GetInternal() {
		t.Fatalf("AddCollaborator entry = %+v, want internal filler", entry)
	}

	// 授权后立即（同一瞬间）可写：create_record/edit_own 均 allow，行级范围 OWN。
	fillerContext := contextWithSharePrincipal(t, principalForUser(internalID, enterpriseID))
	for _, action := range []sharev1.Action{
		sharev1.Action_ACTION_CREATE_RECORD,
		sharev1.Action_ACTION_EDIT_OWN,
	} {
		decision, err := harness.client.CheckAccess(fillerContext, &sharev1.CheckAccessRequest{
			Principal: principalForUser(internalID, enterpriseID),
			BaseId:    baseID,
			Action:    action,
		})
		if err != nil {
			t.Fatalf("CheckAccess(%v) error = %v", action, err)
		}
		if !decision.GetAllow() {
			t.Fatalf("CheckAccess(%v) allow = false, want immediate allow after grant", action)
		}
		if decision.GetRowScope() != sharev1.RowScope_ROW_SCOPE_OWN {
			t.Fatalf("CheckAccess(%v) row_scope = %v, want OWN", action, decision.GetRowScope())
		}
	}

	// H-64：外部协作者授 filler 成功（外部上限 editor > filler），manager 仍拒。
	externalAdded, err := harness.client.AddCollaborator(managerContext, &sharev1.AddCollaboratorRequest{
		BaseId:   baseID,
		Username: "ext_filler",
		Role:     sharev1.Role_ROLE_FILLER,
	})
	if err != nil {
		t.Fatalf("AddCollaborator(external filler) error = %v", err)
	}
	if externalAdded.GetCollaborator().GetInternal() {
		t.Fatalf("AddCollaborator(external filler) = %+v, want external", externalAdded.GetCollaborator())
	}
	if externalAdded.GetCollaborator().GetUsername() == externalID ||
		externalAdded.GetCollaborator().GetDisplayName() == externalID {
		t.Fatalf(
			"AddCollaborator(external filler) username/display_name 落 user_id: %q/%q",
			externalAdded.GetCollaborator().GetUsername(),
			externalAdded.GetCollaborator().GetDisplayName(),
		)
	}
	if _, err := harness.client.AddCollaborator(managerContext, &sharev1.AddCollaboratorRequest{
		BaseId:   baseID,
		Username: "ext_filler",
		Role:     sharev1.Role_ROLE_MANAGER,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("AddCollaborator(external manager) error = %v, want 1001", err)
	}

	// H-63：列表条目恒携带稳定 id 与真实用户名/显示名。
	listed, err := harness.client.ListCollaborators(managerContext, &sharev1.ListCollaboratorsRequest{
		BaseId: baseID,
	})
	if err != nil {
		t.Fatalf("ListCollaborators error = %v", err)
	}
	if len(listed.GetCollaboratorList()) != 3 {
		t.Fatalf("ListCollaborators len = %d, want 3", len(listed.GetCollaboratorList()))
	}
	for _, listedEntry := range listed.GetCollaboratorList() {
		if listedEntry.GetId() == "" || listedEntry.GetId() != listedEntry.GetUserId() {
			t.Fatalf("ListCollaborators entry id = %q (user_id %q), want stable entry id", listedEntry.GetId(), listedEntry.GetUserId())
		}
		if listedEntry.GetUsername() == "" || listedEntry.GetUsername() == listedEntry.GetUserId() {
			t.Fatalf("ListCollaborators username 落 user_id: %+v", listedEntry)
		}
		if listedEntry.GetDisplayName() == "" || listedEntry.GetDisplayName() == listedEntry.GetUserId() {
			t.Fatalf("ListCollaborators display_name 落 user_id: %+v", listedEntry)
		}
	}
	if _, err := harness.forwarder.ForwardOnce(ctx); err != nil {
		t.Fatalf("ForwardOnce(add) error = %v", err)
	}

	// H-65：以条目 id 寻址调整角色为 filler 成功。
	updated, err := harness.client.UpdateCollaboratorRole(managerContext, &sharev1.UpdateCollaboratorRoleRequest{
		BaseId: baseID,
		UserId: externalID,
		Role:   sharev1.Role_ROLE_FILLER,
	})
	if err != nil {
		t.Fatalf("UpdateCollaboratorRole(by entry id) error = %v", err)
	}
	if updated.GetCollaborator().GetRole() != sharev1.Role_ROLE_FILLER ||
		updated.GetCollaborator().GetId() != externalID {
		t.Fatalf("UpdateCollaboratorRole(by entry id) = %+v", updated.GetCollaborator())
	}

	// H-65：以用户名寻址兜底成功（列表真实用户名可直接用于管理操作）。
	if _, err := harness.client.UpdateCollaboratorRole(managerContext, &sharev1.UpdateCollaboratorRoleRequest{
		BaseId: baseID,
		UserId: "ext_filler",
		Role:   sharev1.Role_ROLE_VIEWER,
	}); err != nil {
		t.Fatalf("UpdateCollaboratorRole(by username) error = %v", err)
	}

	// H-66：以条目 id 与用户名寻址移除均成功；未知标识 1001；自身（含用户名寻址）3001。
	if _, err := harness.client.RemoveCollaborator(managerContext, &sharev1.RemoveCollaboratorRequest{
		BaseId: baseID,
		UserId: "ext_filler",
	}); err != nil {
		t.Fatalf("RemoveCollaborator(by username) error = %v", err)
	}
	if _, err := harness.client.RemoveCollaborator(managerContext, &sharev1.RemoveCollaboratorRequest{
		BaseId: baseID,
		UserId: externalID,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("RemoveCollaborator(already removed) error = %v, want 1001", err)
	}
	if _, err := harness.client.RemoveCollaborator(managerContext, &sharev1.RemoveCollaboratorRequest{
		BaseId: baseID,
		UserId: "01J00000000000000000003799",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("RemoveCollaborator(unknown id) error = %v, want 1001", err)
	}
	if _, err := harness.client.RemoveCollaborator(managerContext, &sharev1.RemoveCollaboratorRequest{
		BaseId: baseID,
		UserId: managerID,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("RemoveCollaborator(self) error = %v, want 3001", err)
	}
	if _, err := harness.client.RemoveCollaborator(managerContext, &sharev1.RemoveCollaboratorRequest{
		BaseId: baseID,
		UserId: "manager_name",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("RemoveCollaborator(self by username) error = %v, want 3001", err)
	}
}

// TestListCollaboratorsFilterMatchesRealUsername 锁 q 过滤按真实用户名/显示名/ID 匹配。
func TestListCollaboratorsFilterMatchesRealUsername(t *testing.T) {
	harness := newShareTestHarnessWithPolicy(t, false)
	const (
		baseID       = "01J00000000000000000003801"
		enterpriseID = "01J00000000000000000003802"
		managerID    = "01J00000000000000000003803"
		editorID     = "01J00000000000000000003804"
	)
	harness.baseResolver[baseID] = enterpriseID
	harness.baseOwner[baseID] = managerID
	harness.memberRole[enterpriseID+"/"+managerID] = iamv1.MemberRole_MEMBER_ROLE_MANAGER
	harness.profiles[editorID] = &iamv1.User{
		Id: editorID, Username: "alice", DisplayName: "爱丽丝",
	}
	seedShareCollaborator(t, harness, baseID, editorID, "editor", true)
	managerContext := contextWithSharePrincipal(t, principalForUser(managerID, enterpriseID))

	byUsername, err := harness.client.ListCollaborators(managerContext, &sharev1.ListCollaboratorsRequest{
		BaseId: baseID,
		Q:      "ali",
	})
	if err != nil {
		t.Fatalf("ListCollaborators(q=username prefix) error = %v", err)
	}
	if len(byUsername.GetCollaboratorList()) != 1 ||
		byUsername.GetCollaboratorList()[0].GetUserId() != editorID {
		t.Fatalf("ListCollaborators(q=username) = %+v, want alice entry", byUsername.GetCollaboratorList())
	}
	byDisplayName, err := harness.client.ListCollaborators(managerContext, &sharev1.ListCollaboratorsRequest{
		BaseId: baseID,
		Q:      "丽丝",
	})
	if err != nil {
		t.Fatalf("ListCollaborators(q=display_name) error = %v", err)
	}
	if len(byDisplayName.GetCollaboratorList()) != 1 {
		t.Fatalf("ListCollaborators(q=display_name) len = %d, want 1", len(byDisplayName.GetCollaboratorList()))
	}
	byID, err := harness.client.ListCollaborators(managerContext, &sharev1.ListCollaboratorsRequest{
		BaseId: baseID,
		Q:      editorID,
	})
	if err != nil {
		t.Fatalf("ListCollaborators(q=user_id) error = %v", err)
	}
	if len(byID.GetCollaboratorList()) != 1 {
		t.Fatalf("ListCollaborators(q=user_id) len = %d, want 1", len(byID.GetCollaboratorList()))
	}
	miss, err := harness.client.ListCollaborators(managerContext, &sharev1.ListCollaboratorsRequest{
		BaseId: baseID,
		Q:      "nobody",
	})
	if err != nil {
		t.Fatalf("ListCollaborators(q=miss) error = %v", err)
	}
	if len(miss.GetCollaboratorList()) != 0 {
		t.Fatalf("ListCollaborators(q=miss) len = %d, want 0", len(miss.GetCollaboratorList()))
	}
}
