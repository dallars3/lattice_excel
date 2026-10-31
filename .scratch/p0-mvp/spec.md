# P0 最小可用闭环（P0 MVP）

Status: ready-for-agent

## Problem Statement

一个面向中小企业团队的协作多维表格，需要在**单机自托管、零备案、零付费/企业资质外部依赖**的试运行约束下，交付 P0 可用的多人协作闭环：账号与授权、表格结构建模、记录读写与实时协同、评论与站内通知、分享治理、导入导出、以及运营者与审计能力。当前仅有需求与设计文档、proto 契约与三进程 Go 骨架，尚无业务实现。

## Solution

按已评审的三进程架构实现全部 P0：`excel_web_api`（轻量网关节流：会话识别、统一鉴权、协议转换、最小聚合，零 MySQL）、`excel_core_service`（业务主域：iam/share/audit/workspace/structure/records/calc/comment，93 个 gRPC）、`excel_worker_service`（异步 IO 域：file/importexport/notify/scheduler，17 个 gRPC + 内部任务）。以接口文档为唯一接口事实源，按命名基线与文件结构/代码规则落地，最终通过 HTTP 对外行为、全部 gRPC 接口、WebSocket 帧三层测试缝验收。

## User Stories

1. As a 未登录用户, I want to register with username and password, so that 我能建立可在本产品使用的身份。
2. As a 注册用户, I want to log in and stay logged in for 30 days, so that 我不必频繁重复登录。
3. As a 注册用户, I want my username to be globally unique and immutable, so that 协作者能稳定地识别我。
4. As a 站点直注册用户, I want my registration to create an enterprise as owner, so that 我能立即开始工作。
5. As a 持邀请链接注册用户, I want to join the inviting enterprise directly, so that 我不必手动选择企业。
6. As a 持「任何人」链接注册用户, I want to register as an enterprise-less account and return to the Base, so that 我仅以外部协作者身份使用。
7. As an admin, I want to toggle public registration globally, so that 我能控制注册面与滥用风险。
8. As a user, I want to change my password with the old one, so that 我能安全改密；忘记密码不提供找回。
9. As a user, I want to update my display name and avatar, so that 协作展示更友好。
10. As a user, I want to request deactivation with a 7-day cooldown and be able to revoke it, so that 注销可反悔且有缓冲。
11. As an enterprise owner, I want to invite members by username (including pre-inviting unregistered names), so that 团队可以预先建立名单。
12. As an invitee, I want to accept or reject an invitation, so that 我能控制加入哪个企业。
13. As an enterprise manager, I want to list members and adjust roles, so that 我能管理团队权限。
14. As an enterprise manager, I want to disable/enable members, so that 离职或误操作可控（停用后资产归 owner）。
15. As an enterprise owner, I want to transfer ownership with confirmation, so that 交接有继任者且不可自降为无 owner。
16. As an admin, I want RPC-only operations (register switch, emergency password reset, owner transfer, enterprise dissolution acceptance) with a whitelist, so that 运营动作不外露且全程留审计。
17. As a user, I want a home page grouped by recent/created/shared/starred plus invitation todos, so that 我能快速定位工作。
18. As a user, I want to search bases by name, so that 我能快速打开目标 Base。
19. As a Base owner, I want to create/rename/duplicate/delete a Base, so that 我能管理自己的资产。
20. As an enterprise manager, I want to take over or transfer any Base, so that 离职交接不停摆。
21. As a manager, I want to create/rename/duplicate/delete tables within a Base, so that 我能建模数据。
22. As a manager, I want to create fields of 11 types with per-type config, so that 我能表达业务结构。
23. As a manager, I want to rename/convert/duplicate/delete fields with impact previews, so that 结构变更可预期。
24. As a manager, I want to maintain single/multi select options (add/rename/recolor/reorder/delete), so that 选项统一归口。
25. As an editor, I want to control the "allow editing options on input" switch, so that 新选项是否可由编辑者创建符合治理要求。
26. As a manager, I want to create views with filters/sorts/group/visible/frozen config, so that 同一数据按角色组织。
27. As a reader, I want to create/rename/duplicate/delete views (at least one retained), so that 我能组织我的查看方式（P0 无私有视图）。
28. As a viewer, I want a formula field over 26 functions with dependency/cycle detection, so that 我能派生计算值。
29. As a manager, I want single-direction relation fields and Lookups restricted by target-row visibility, so that 关联不泄露数据。
30. As a member, I want to create/read/update/delete records with type validation and soft limits, so that 数据可靠。
31. As an editor, I want optimistic-lock conflict resolution with three choices and no silent overwrite, so that 并发编辑不丢改动。
32. As an editor, I want batch/paste updates with skipped-cell counting, so that 只读列与不匹配选项有明确反馈。
33. As an editor, I want 7-second undo for deletion, so that 误删可即时撤销。
34. As an editor, I want record change history with per-field rollback, so that 错误可追溯可纠正。
35. As a filler, I want to see only my own records, so that 外部填报可见范围受控。
36. As a member, I want relation/lookup candidates filtered by visibility, so that 我无法借关联窥探无权限数据。
37. As a collaborator, I want real-time presence/cursor/"editing" indicators under 1s, so that 多人协作彼此可见。
38. As a collaborator, I want automatic save with save state, so that 无保存按钮焦虑。
39. As an offline user, I want read-only offline and queued resend of pre-disconnect edits, so that 网络恢复后不丢内容。
40. As a collaborator, I want comment/reply/two-level threads with @mentions and resolve state, so that 讨论锚定在记录上。
41. As a user, I want an in-app notification center covering the defined event matrix, so that 协作事件可追赶且不依赖站外触达。
42. As a Base manager, I want to add/remove/change collaborator roles with external caps at editor, so that 权限被精确控制。
43. As a Base manager, I want link sharing with scope and role constraints, so that 外部分享可治理。
44. As a Base manager, I want to close/reset/force-close links, so that 分享引爆后可熔断。
45. As an enterprise manager, I want to force-close any "anyone" link, so that 企业级治理可兜底。
46. As an external collaborator, I want to log in and access shared bases per granted/link role, so that 我能跨企业协作。
47. As an external collaborator, I want export to be denied server-side in all cases, so that 数据外流风险按产品承诺治理（威慑性）。
48. As a manager, I want to import xlsx/xls/csv via preview-then-confirm with type inference and an error report, so that 迁移数据可验证。
49. As an internal member with editor/manager role, I want to export the current view as xlsx/csv, so that 数据可携带。
50. As a user, I want to upload/download/thumbnail attachments with blacklist and quota, so that 文件附件安全受控。
51. As an owner/manager, I want an enterprise audit log (action-level, 180 days, immutable, CSV export), so that 关键动作可问责。
52. As an admin, I want enterprise dissolution via manual acceptance with a 30-day cooldown and owner revoke, so that 企业注销有缓冲可逆。
53. As a user, I want account deactivation to handle asset reassignment and anonymize authorship, so that 注销后数据仍可理解。
54. As a mobile user, I want view/comment/notify only (no edits), so that 移动端轻量使用。
55. As an operator, I want all client-facing writes to enforce rate limits and capacity constants, so that 单机不被滥用。
56. As a developer maintainer, I want every interface classified P0/P1/P2 with declared idempotency/fallback/dependencies, so that 运维与回归有依据。

## Implementation Decisions

- 三进程部署、唯一公网面 `excel_web_api`（零 MySQL）、两个 RPC 进程仅 localhost。
- 服务间契约：13 个 proto 包（common + 8 core + 4 worker + webapi WS 帧契约），以接口文档为唯一事实源。
- 鉴权：HTTP 网关 ExchangeSession 后统一 `share.CheckAccess`，通过才转发业务 RPC；业务 RPC 仅做 principal + row_scope 行级过滤。
- 权限：静态角色→动作矩阵置于独立 JSON 配置仓库（加载+热更）；动态授权入 MySQL；显式与链接取高、外部上限 editor、外部禁导出。
- 命名字约定：admin/manager/operator/author、_list/_map、LinkContext、服务与 proto 命名按整体方案执行。
- 数据存储：单 MySQL，各模块表前缀 excel_<模块名>_；业务 ID 用 ULID；附件本地磁盘 5GB 池。
- base 层按模块分文件，A 模块只操作 A 模块表；跨模块在逻辑层拆分/聚合，确需跨模块 SQL 注释原因。
- 写可靠性：业务写事务内 outbox；EventBus = Redis PubSub（瞬时）+ Streams（可靠）；event_id 幂等；Kafka 可切换。
- 实时：WS 纯内存房间，last_seq 补发对账；会话/鉴权缓存 TTL<60s，事件主动失效满足 1 分钟熔断。
- 计算：公式/引用/Lookup 后端权威物化；同行同步、跨行经 calc.job 拓扑异步重算。
- 异步任务：MySQL 任务表 + FOR UPDATE SKIP LOCKED 租约 + 重试/死信。
- 跨服务清清理：企业注销到期发布 E-17，worker 三模块幂等清理。
- admin 接口仅 RPC（localhost 脚本），动作入审计。
- 错误模型：统一 {code,message,data}，错误码与映射按接口文档。
- 文件流：上传两段式（InitUpload 客户端流 + ConfirmUpload）；下载/缩略图/导出/报告服务端流；usage=import_temp 管临时文件。
- 移动端：写入口不开，服务端不按 UA 限权。
- 代码规则：一文件一接口；泛型层按模块分文件、复用归 common.go、配置 *_config；函数名体现用途、单一职责、≤500 行可例外。

## Testing Decisions

- 测试缝共三层，全部按外部行为验收：HTTP 面 93 接口；gRPC 面 core 8 服务 93 RPC + worker 3 服务 17 RPC 逐个建契约测试；WS 面 W-01~W-16 按 protojson 帧断言。
- 好测试标准：只测外部行为（响应结构/错误码/事件副作用/幂等窗口），不断言 SQL/内部函数；副作用经事件与审计接口观察。
- 被测模块：web_api gateway/realtime；core 8 模块；worker 4 模块；EventBus 发布-消费幂等。
- 依据与先例：接口文档清单 + §9 契约测试清单 + 三份服务方案的 P0/P1/P2 与幂等/兜底档；工具优先 curl/grpcurl + JSON 断言；性能压测单列、不入功能测试。
- 回归触发：接口或 proto 变更须重跑受影响测试缝；允许先单缝后全量。

## Out of Scope

- P1 全套与 P2 全部（表单、看板/日历/甘特、仪表盘、自动化、模板、回收站、字段级权限、高级字段、全局搜索、链接密码/有效期、选项合并、二维粘贴、批量编辑等）。
- 明确不做：传输加密、HTTPS、域名备案、第三方登录/短信/邮件/推送/验证码/CDN、商业表格内核与公式引擎、移动端编辑、多企业单账号。

## Further Notes

- 唯一事实源同步纪律：接口文档/proto/三份服务方案/PRD 变更须同步本规格。
- 分级与幂等/兜底策略、文件结构规则、代码编写规则已固化，作为 Code Review 必检项。
- 内网 gRPC 信任网关注入 principal；若未来直连暴露须恢复 RPC 侧鉴权。
- 备份运维是运营手册职责；admin 通道以脚本交付并审计留痕。
