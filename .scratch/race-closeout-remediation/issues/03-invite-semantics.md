# 03: 邀请流语义修复（N7 收尾）

**What to build:** 注册自动建企（N7）落地后，企业成员邀请流对几乎所有用户失效（新注册用户必然已自有企业，而邀请/接受的前置是「用户无企业归属」）。修复为：按用户名可邀请任何活跃用户；接受邀请即为目标企业新增成员行（角色按邀请授予），**不修改用户的企业字段、不迁移不清空既有 Base**；企业内鉴权按成员行判定。实现票必须先验证现有鉴权链在「用户以成员行归属（非当前企业字段）」场景下对目标企业 Base 的访问是否成立，不成立则修正鉴权。

**Blocked by:** None (can start immediately).

**Status:** ready-for-human

- [x] 移除邀请与接受两处「目标用户须无企业归属」前置，允许邀请任何活跃用户
- [x] 接受邀请 = 目标企业新增成员行（角色按邀请授予），不修改用户企业字段、不迁移既有 Base、不破坏自有数据访问
- [x] 实现前先验证跨企业鉴权链：用户以成员行归属访问目标企业 Base 是否成立，不成立则修正为企业内鉴权按成员行判定（验证过程记录）
- [x] Go 集成用例：邀请自有企业用户→接受→以成员身份访问目标企业 Base 全链路
- [x] 重复接受幂等（同一用户同一企业不重复建成员行）；外部协作者（链接令牌）路径行为不变
- [x] 既有成员生命周期用例回归绿（含此前改直建孤立账号的夹具，已按新语义简化）

## Comments

### 2026-11-20 实现（fix/rc-03-invite-semantics，a6b9944..3e71ff5）

**鉴权链验证记录（实现前，票面要求）：**

- 会话主体携带 `users.enterprise_id`（internal/iam/common.go:348-362 `sessionPrincipal`，exchange_session.go:40-47）。
- 修复前 share/common.go `resolveDecision` :372-378：`internal := principal.GetEnterpriseId() != "" && principal.GetEnterpriseId() == enterpriseID(base)`；成员行仅在 internal 分支内查询（MemberRoleProvider → base/iam.go ActiveMemberRole）。**结论：跨企业访问 DENIED**——用户持有目标企业 A 的成员行但企业字段 = B 时，internal=false，成员行不参与判定，角色只来自协作者行（外部封顶，无导出）或空（拒绝）。share 层 red 用例（identity_resolution_test.go 新增）复现实测：`ResolveRole() = {role:ROLE_EDITOR internal:false}, want internal:true`。
- **修复**（fix(share) 3946d18）：internal 分类改为「用户在 Base 所属企业存在有效成员行」（MemberRoleProvider.Role，fail-closed）；协作者推导/所有者覆盖/链接合并/外部封顶/企业链接范围不变。

**实现：**

- 迁移 000020_invite_semantics（up/down，守卫式 prepared-statement IF）：`excel_iam_invitations` 增 `role` 列（默认 member，存量回填）；删除 `uk_excel_iam_members_user_id` 单企业归属唯一键。
- invite_member.go：前置改为「非活跃目标拒绝（该用户当前不可被邀请）+ 已是本企业活跃成员拒绝（该用户已是本企业成员）」，均 1002；成员行校验同事务（ActiveMemberRoleTx）。
- accept_invite.go：移除「用户无企业」前置与 SetUserEnterprise；成员行按邀请角色插入（invitationGrantedRole，仅 member/manager 合法否则兜底 member）；幂等——同一已接受邀请重复接受 = 成功空操作；已有活跃成员行 = 视为已加入（邀请置 accepted、不重复建行、不计数）；非活跃成员行仍 1002。
- base/iam.go：`EnterpriseAndRoleByUserID`/`MemberByUserID` 按用户自有企业过滤（多成员行不再读任意行）；`EnsureDefaultEnterprise` 增成员行守卫（孤立账号受邀加入后登录不得补建默认企业，与用户锁同事务，red→green 已验证：无守卫时 Login 补建出虚假企业）。

**测试证据：**

- red：share 层成员行用例（internal:false≠true）；bufconn 集成用例首轮 `InviteMember() error = AlreadyExists 该用户已属于其他企业`（票面所指的坏前置实测在案）。
- green：新集成用例 invite_cross_enterprise_test.go 全链路（邀请自有企业用户→接受→成员行 member/active→GetMe 仍自有企业 owner→member_count=2→重复接受幂等→再次邀请活跃成员拒绝→目标企业 Base 协作者 editor+成员行 = 内部身份导出放行，外部协作者导出仍拒绝→自有企业 Base 访问不受损）。
- 全量：`go test -p 1 -count=1 ./...`（excel_core_service）15 包全绿；excel_worker_service / excel_web_api `go build ./...` 通过；`git merge fix/race-closeout-remediation` no-op。

**旧语义用例收编：** enterprise_members_test.go（邀请流改延迟绑定+注册后接受、停用/启用改按成员行与自有会话断言、角色守卫/事件/分页夹具简化为注册用户）、admin_lifecycle_test.go（管理转移按成员行+Provider 断言、注销移交/事件按自有企业断言）、enterprise_directory_test.go、iam_integration_test.go（新增孤立成员不补建用例）。孤立夹具仅保留给补建专项用例。

**遗留提示（超出本票，未改）：** ① `docs/doc/接口文档-v0.7.md` B-01 行仍写旧文案「该用户已属于其他企业」（契约已变：属他企业可邀，仅本企业活跃成员/非活跃目标 1002）；② excel_web_api gateway 透传测试 iam_routes_test.go:638-659 桩字符串引用旧文案（桩驱动，不失效）；③ 注销（ExecuteDeactivation）资产移交与成员行停用仍按用户企业字段口径，跨企业成员行的资产移交属票 09 Base 装配范围（已在测试注释标注）。
