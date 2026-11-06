// workspace/modifier_name_test.go：缺陷 #28（H-21 卡片最近修改人显示内部 user_id）。
// 锁定行为：Base 摘要 last_modifier 返回可读身份——显示名优先、缺失回退用户名、
// 资料缺失或 provider 故障保留原值（H-21 RW/P 聚合的展示字段降级不阻塞主链路）。
package workspace_test

import (
	"context"
	"testing"

	iamv1 "excel/api/iam/v1"
	workspacev1 "excel/api/workspace/v1"
)

// fakeProfileProvider 以固定映射模拟 IAM 账号资料（对齐 ShareUserLookup 语义：
// 查无账号的 id 直接缺席结果）。
type fakeProfileProvider struct {
	profiles map[string]*iamv1.User
	err      error
}

func (provider *fakeProfileProvider) ProfilesByIDs(
	_ context.Context,
	userIDs []string,
) (map[string]*iamv1.User, error) {
	if provider.err != nil {
		return nil, provider.err
	}
	result := make(map[string]*iamv1.User, len(userIDs))
	for _, id := range userIDs {
		if profile := provider.profiles[id]; profile != nil {
			result[id] = profile
		}
	}
	return result, nil
}

// TestHomeModifierShowsReadableName 缺陷 #28：H-21 四列表的 last_modifier 必须
// 是可读显示名（缺失回退用户名），不允许出现内部 user_id。
func TestHomeModifierShowsReadableName(t *testing.T) {
	profiles := &fakeProfileProvider{profiles: map[string]*iamv1.User{
		testOwner:   {Id: testOwner, Username: "t16_owner", DisplayName: "张三"},
		testManager: {Id: testManager, Username: "t16_manager"},
	}}
	harness := newWorkspaceHarnessWith(t, nil, nil, profiles)
	ownerCtx := authenticatedContext(t, testOwner, testEnterprise)
	managerCtx := authenticatedContext(t, testManager, testEnterprise)

	owned, err := harness.client.CreateBase(ownerCtx, &workspacev1.CreateBaseRequest{
		Name: "t16 修改人-自有",
	})
	if err != nil {
		t.Fatalf("CreateBase(owned) error = %v", err)
	}
	shared, err := harness.client.CreateBase(managerCtx, &workspacev1.CreateBaseRequest{
		Name: "t16 修改人-共享",
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
	// 第三条摘要的修改人在资料源中不存在（账号已注销等边缘）：无 profile →
	// 原值兜底，绝不允许编造空名。
	unknownBase, err := harness.client.CreateBase(managerCtx, &workspacev1.CreateBaseRequest{
		Name: "t16 修改人-无资料",
	})
	if err != nil {
		t.Fatalf("CreateBase(unknown) error = %v", err)
	}
	unknownModifier := "01J00000000000000000000999"
	if _, err := harness.db.ExecContext(
		context.Background(),
		`UPDATE excel_workspace_bases SET last_modifier = ? WHERE id = ?`,
		unknownModifier,
		unknownBase.GetBase().GetId(),
	); err != nil {
		t.Fatalf("set unknown last_modifier: %v", err)
	}
	if _, err := harness.db.ExecContext(
		context.Background(),
		`INSERT INTO excel_share_collaborators
			 (base_id, user_id, role, internal, source, granted_at)
			 VALUES (?, ?, 'editor', 1, 'invite', ?)`,
		unknownBase.GetBase().GetId(),
		testOwner,
		harness.now.UnixMilli(),
	); err != nil {
		t.Fatalf("insert unknown-base collaborator: %v", err)
	}

	// CreateBase 响应自身就是 Base 摘要：principal 资料应已解析为显示名。
	if got := owned.GetBase().GetLastModifier(); got != "张三" {
		t.Fatalf("CreateBase last_modifier = %q, want 张三", got)
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
	assertModifier(t, home.GetCreatedList(), owned.GetBase().GetId(), "张三")
	assertModifier(t, home.GetSharedList(), shared.GetBase().GetId(), "t16_manager")
	assertModifier(t, home.GetSharedList(), unknownBase.GetBase().GetId(), unknownModifier)
}

// TestSearchBasesModifierShowsReadableName 缺陷 #28：H-22 搜索结果同样渲染在
// 首页卡片上，last_modifier 必须同为可读身份。
func TestSearchBasesModifierShowsReadableName(t *testing.T) {
	profiles := &fakeProfileProvider{profiles: map[string]*iamv1.User{
		testOwner: {Id: testOwner, Username: "t16_owner", DisplayName: "张三"},
	}}
	harness := newWorkspaceHarnessWith(t, nil, nil, profiles)
	if _, err := harness.client.CreateBase(
		authenticatedContext(t, testOwner, testEnterprise),
		&workspacev1.CreateBaseRequest{Name: "t16 搜索修改人"},
	); err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	result, err := harness.client.SearchBases(
		authenticatedContext(t, testOwner, testEnterprise),
		&workspacev1.SearchBasesRequest{Q: "t16 搜索修改人"},
	)
	if err != nil {
		t.Fatalf("SearchBases() error = %v", err)
	}
	if len(result.GetBaseList()) != 1 {
		t.Fatalf("search count = %d, want 1", len(result.GetBaseList()))
	}
	if got := result.GetBaseList()[0].GetLastModifier(); got != "张三" {
		t.Fatalf("search last_modifier = %q, want 张三", got)
	}
}

// TestHomeModifierKeepsRawWhenProfilesFail 锁定降级语义：provider 故障时 H-21
// 仍成功返回，last_modifier 保留原值而不是报错或清空。
func TestHomeModifierKeepsRawWhenProfilesFail(t *testing.T) {
	profiles := &fakeProfileProvider{err: context.DeadlineExceeded}
	harness := newWorkspaceHarnessWith(t, nil, nil, profiles)
	created, err := harness.client.CreateBase(
		authenticatedContext(t, testOwner, testEnterprise),
		&workspacev1.CreateBaseRequest{Name: "t16 降级"},
	)
	if err != nil {
		t.Fatalf("CreateBase() error = %v", err)
	}
	result, err := harness.workspaceServer.LoadHome(
		authenticatedIncomingContext(t, testOwner, testEnterprise),
	)
	if err != nil {
		t.Fatalf("LoadHome() error = %v, want success with raw fallback", err)
	}
	assertModifier(t, result.Home.GetCreatedList(), created.GetBase().GetId(), testOwner)
}

func assertModifier(
	t *testing.T,
	list []*workspacev1.Base,
	baseID, want string,
) {
	t.Helper()
	for _, item := range list {
		if item.GetId() != baseID {
			continue
		}
		if item.GetLastModifier() != want {
			t.Fatalf("base %s last_modifier = %q, want %q", baseID, item.GetLastModifier(), want)
		}
		return
	}
	t.Fatalf("base %s not found in list of %d", baseID, len(list))
}
