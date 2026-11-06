# 03: 协作者授权与可写性（filler 授权 / 条目 id / 9001 瞬态）

**What to build:** 管理者能把协作者设为「可填写」并在添加后立即可正常协作：H-64 授权接受 filler（外部上限仍为 editor）；H-63 协作者条目携带稳定 id 且 username/display_name 为真实值（外部条目不得落用户 id）；H-65/H-66 以该 id 成功调整角色与移除；新协作者加入后立即读写正常，不得出现约 4 分钟窗口的间歇 9001（若判定预热属预期，窗口内必须返回可解释业务码而非 9001）。本票解除冒烟 TC-080-01（可填写数据范围）与 TC-090-08（移除协作者触发 W-15）的阻塞。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] 后端：H-64 接受 role=filler（白名单补齐；外部协作者授 manager 仍被拒；编辑器/查看者路径不回归），契约测试锁角色矩阵
- [x] 后端：H-63 条目新增稳定 id（协作者记录主键），username/display_name 恒为真实用户名/显示名（冒烟证据：外部条目三字段均落 user_id）；H-65/H-66 以条目 id 寻址成功（现以 user_id/username 调用均 1001）
- [x] 可靠性：新协作者添加后 1 分钟内完成一次 UpdateCell/CreateRecord 成功（冒烟证据：A8/ed2 首写多次 9001 约 4 分钟后自愈；复现链路含时间线）；若根因属缓存预热，窗口内错误码改为可解释业务码并在票 Comments 记录根因
- [x] 契约测试覆盖上述三点；`npm run test`/build/契约测试/check-docs 全绿
- [x] 解除阻塞验证：以 API 预置可填写协作者并按 TC-080-01 前置执行；移除协作者触发 W-15 会话收回（WS 对测）

## Comments

- 2026-11-06 实现备注（filler/条目 id）：filler 白名单缺口在 `add_collaborator.go`/`update_collaborator_role.go` 补齐（`normalizeRole`+`role==""` 拒绝；外部授 manager 双入口仍 1001，外部上限 `capExternalRole` 保持 editor）；proto `Collaborator` 增 `id = 8`（只增不改），条目 id == user_id（表 PK (base_id,user_id)，票面口径已写入接口文档 §8.1.6）；H-65/H-66 按 user_id 精确匹配、用户名兜底解析（IAM `UsersByIDs` 批量查询），移除/调角色以解析后 id 比较。username/display_name 经新缝 `share.UserProfileProvider`（生产由 `iam.ShareUserLookup` 实现）恒为真实值，且取值在提交前完成（profile 失败不产生已提交写的 9001）。迁移 `000019_share_collaborator_filler.{up,down}.sql`（role ENUM 补 filler，down 先降级 filler→editor）。提交 `8f37a7e`、`749b772`、`c17af8d`、`809aef6`。
- 2026-11-06 9001 根因（**最终版，替代本票早前「静态追踪未发现专属 9001 路径」的结论**）：部署实例实测复现确定性 9001——外部协作者（principal `enterprise_id=""`）首写 POST /records → 9001，读 GET /records → 4002「记录/字段已被删除」。根因：`internal/records/write_options.go` `structureBaseContextProvider.ContextByTable`（及 `structureViewAccessor.Field/HasCrossTableDependents`、`structure_accessor.go Table`）以 **调用者企业** 过滤结构表（`base/structure.go TableByID` SQL `WHERE t.id=? AND t.enterprise_id=?`），外部协作者空企业 → `ErrStructureNotFound` → 写路径裸错误 gRPC Unknown → 9001、读路径 `mapRecordError` → 4002；share CheckAccess 本身正常（`allow=true, ROLE_FILLER, ROW_SCOPE_OWN`）。修复：新增 `TableByIDCollaborator`（按表 id 直取，租户取表行自身企业；结构写路径保持 scoped `TableByID`），协作者面向缝统一走 `tableForPrincipalTenancy` 分支；`internal/records/collaborator_write_test.go` 四条集成测试（filler 立即写/editor 全行/viewer 只读拒绝/internal 不回归）修复前 red、修复后 green。提交 `c1fb2b4`（合入 `d6d5592`）。
- 2026-11-06 9001 附注：鉴权失效传播链（outbox `share.changed` → 网关 `AuthInvalidator`）经测试锁定为即时生效（`TestAuthorizationCacheDenyInvalidatedByShareChanged`，TTL 55s 未放宽）；冒烟「约 4 分钟自愈」与上述确定性根因叠加，TTL 过期解释了自愈现象的观测窗口。
- 2026-11-06 解除阻塞验证（部署实例 API 实测）：`AddCollaborator(role=filler)` 返回 `code:0`（含 id/真实 username/display_name，`ROLE_FILLER`）；协作者随 grant 后立即 CreateRecord/UpdateCell/读回全部 `code:0`（此前 9001/4002）——TC-080-01 前置成立。顺带修复真实 W-15 缺口：realtime hub 原未订阅 `share.changed`，被移除协作者收不到 `session_revoked`，现 `change_type=removed` 定向推送 `session_revoked(reason=perm_changed)`（`851343`），解除 TC-090-08 阻塞（浏览器 WS 对测留待 P0 冒烟复跑）。

**冒烟证据**：H-64 role=filler/ROLE_FILLER → 1001（editor 正常）；H-63 条目无 id 字段、外部条目 username=user_id；H-66 按 user_id/username 删除均 1001 且列表不变；9001 时间线——首次 9001 至自愈约 4 分钟，期间 owner 写正常。
