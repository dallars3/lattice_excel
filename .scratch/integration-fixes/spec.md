# P0 终检收口：最小 APPROVE 清单

Status: ready-for-agent

## Problem Statement

P0 MVP 的 18 张实现票已全部合并，三服务构建与既有契约测试通过，但最终集成评审判定为 NEEDS_FIX：尚有 4 个 P1 问题未达 APPROVE 基线。接口文档 §2 的 93 个对客 HTTP 端点中有 63 个未在网关注册，调用只会得到「未注册/404」，另有 5 条实现新增却未入文档的路由；导入向导只支持 .xlsx/.csv，选 .xls 无法预览与入库，与产品承诺不一致；账号申请注销（7 天冷静期）与企业注销受理（30 天冷静期）到期后没有任何 intake 通道把到期信息送入 worker 任务表，生效动作无人领取执行，运营需要人工插任务数据；企业管理器强制关闭 anyone 链接时，同一动作经链接关闭事件投影与显式审计写出两条 AUDIT_ACTION_LINK_FORCE_CLOSED。收口目标是把这 4 项全部修复，使契约门（HTTP 未注册=0、额外=0、文档匹配=93）与文档门全部通过，最终评审可判 APPROVE。

## Solution

分四条主线收口，全部复用既有架构与验收缝，不新开部署形态：网关对文档内 93 个端点全量注册并治理 5 条额外路由；导入管线为 .xls 增加解析适配、复用同一预览-确认-报告-通知闭环；为两类到期动作建立 core 到 worker 任务表的事件投递 intake，到期任务由既有调度器领取执行且幂等；强制关闭链接改为审计单写出、以事件投影为唯一路径并明确去重指纹。实现与文档同步变更，全程由三服务回归、契约测试与文档护栏收口。

## User Stories

1. As a 对客用户, I want 接口文档 §2 所列 93 个端点全部真实可调, so that 我按文档使用任何接口都成功并收到明确业务结果。
2. As a 企业管理员, I want 企业、成员、邀请与分享治理端点全部被网关转发到 core, so that 我的管理操作不失联。
3. As a 数据导入者, I want 导入、导出与附件端点全部转发到 worker, so that 文件类操作不出现死胡同。
4. As an 审计口负责人, I want 运行时路由清单与文档逐项一致（未注册=0、额外=0、文档匹配=93）, so that 契约审计只有一份事实源。
5. As a developer, I want 每条新增路由要么并入文档、要么改成文档已有路径形态, so that 实现与文档永不漂移。
6. As a 导入者, I want 选择 .xls 时获得与 .xlsx 相同的解析预览（列类型与首行字段名提示）, so that 我不必先转格式再导。
7. As a 导入者, I want 确认 .xls 导入后所有行写入、坏行跳过并续行, so that 局部坏数据不阻塞迁移。
8. As a 导入者, I want 得到可下载错误报告逐行说明原因, so that 我能修复后重导。
9. As a 导入者, I want 导入完成后收到 import.finished 通知（字段齐全含企业标识）, so that 我能及时跟进且可供审计消费。
10. As a developer, I want .xls 复用既有校验、报告与通知契约、仅换解析适配, so that 三条格式路径行为一致而不是三份实现。
11. As a 申请注销的用户, I want 我的申请 7 天到期后系统自动执行生效, so that 承诺按时兑现且不依赖人工。
12. As a 企业所有者, I want 企业注销受理 30 天冷静期到期后自动销毁, so that 收尾不因无人插任务而停滞。
13. As a 申请注销的用户, I want 冷静期内撤回仍然有效, so that 自动生效不会吞掉反悔窗口（撤回后到期任务不再执行）。
14. As an 运营者, I want 到期执行幂等（重复触发只生效一次）, so that 重试或重放不会重复破坏数据。
15. As a developer, I want 到期信息从 core 到 worker 任务表有明确的 intake 契约, so that runner 能领取执行且不新增 RPC（110 计数不变）。
16. As a developer, I want 同一 intake 的重复投递只产生一条可执行任务, so that 任务表不因重放堆积。
17. As a developer, I want 到期任务失败沿用既有退避与死信语义, so that 运维追踪口径统一。
18. As a 企业管理员, I want 强制关闭外部链接后审计恰好一条 LINK_FORCE_CLOSED, so that 问责记录不重复不失真。
19. As an 审计者, I want Query 强关链接触发的那次动作恰好可见一条, so that 报表数字可信。
20. As a developer, I want 去重依据（event_id 或 reason 指纹）写入实现与文档, so that 未来新消费路径不会再次双写。
21. As a Base 管理者, I want 普通关闭、重置与收窄链接的审计行为保持不变, so that 修复只影响强关双通道。
22. As an 外部协作者, I want 强关后立即失去访问（既有吊销语义不回退）, so that 治理动作依然即时生效。
23. As a developer 维护者, I want 每个修复都带接口号可定位的契约测试, so that 失败一眼看到是哪个 H/W/RPC 与差异。
24. As a developer 维护者, I want 每张票合并后三服务回归与契约、文档闸全绿, so that 收口过程中不引入回归。
25. As a 发布负责人, I want 终检最小 APPROVE 清单全部 done 且有统一验收口径, so that 放行决策有事实依据。

## Implementation Decisions

- 接口文档 §2 仍为 HTTP 面唯一事实源，契约门口径固定为「未注册=0、额外=0、文档匹配=93」；后端 RPC 若真实未实现，允许按后端真实状态返回，但网关管线不得缺席（不出现 404）。
- 缺失的 63 个端点全部经统一网关管线注册并转发既有 core/worker RPC；5 条额外路由二选一治理：并入接口文档并同步，或改为文档既有路径形态。
- .xls 走既有两段式导入（预览后确认）与类型推断、首行字段提示、坏行跳过、报告文件、import.finished 契约，新增 BIFF 解析适配器；xlsx/csv 路径不动。
- lifecycle intake 采用事件投递契约：core 在受理注销申请与企业注销时随业务事务发布到期事件（携带 user_id / enterprise_id 与 deadline），worker 消费端可靠接收后写入任务表（run_at=deadline）；调度器按既有 ClaimDue 语义领取，到期调用 ExecuteDeactivation / ExecuteEnterpriseDissolution。
- 不新增 RPC，proto 110 条计数不变；新增事件与字段按唯一事实源纪律入接口文档 §5/§8 清单并同步本规格。
- intake 幂等指纹 = 事件类型 + 主体标识 + 冷静期世代；重复投递仅产生一条 pending 任务；执行侧 RPC 保持幂等，重复触发只生效一次；失败沿用退避与死信。
- 强关审计改为单写出：以链接关闭事件投影为唯一审计路径，移除 ForceCloseByManager 内的显式审计追加；事件消费者按 reason=manager_force_close 甄别才映射 LINK_FORCE_CLOSED，普通关闭、重置与收窄行为不变；审计行以事件 event_id 为幂等指纹。
- 文档同步纪律：本次涉及的路由、事件、审计约定变更与实现同批落地；需要新增唯一约束时走既有迁移编号、up-down 成对、表名全局唯一栅栏。

## Testing Decisions

- 验收继续用既有三层外部行为缝（HTTP 面 93、gRPC 面 110、WS 面 16），contract 报告为本收口的门禁；新增测试并入既有 harness，不新开测试系统。
- 时间缝隙集中在 scheduler：沿用可注入时钟验证 run_at 到期领取与幂等，不把 7/30 天做成真实等待。
- 好测试标准：只断言外部行为与可观察副作用——HTTP 状态码、业务码与转发到位；preview 的 detected_type 与 first_row_header_hint；导入行数、续行、报告文件与 import.finished 负载；到期任务出现、Execute* 被调用、重复 intake 单条任务；审计 Query 恰好一条。不断言 SQL、内部函数或解析实现细节。
- 被测模块：web_api 网关路由；worker importexport 与 scheduler intake/领取；core iam 事件发布、share 强关与 audit 投影/去重。
- 依据与先例：contract-coverage.md 矩阵与 contract-test.ps1；core/worker 既有 scheduler、audit consumer、importexport 与 share 治理测试；接口文档 §9 契约测试清单口径。
- 回归触发：每张票合并后先跑受影响服务（-p 1）再跑全量 contract-test.ps1 与 check-docs.ps1；四张票全部合并后做一次终检前全量回归。

## Out of Scope

- P0 之外的全部 P1/P2 待办（表单、看板/日历/甘特、仪表盘、自动化、模板、回收站等）不因本收口提前开工。
- 已修复的 core copylock 问题（原 P1-1）不重复实现，仅维持 contract-test.ps1 中的 go vet 回归闸。
- retro 尚未排期的事后项（CODING_STANDARDS.md 落地、implementation 运行手册指引、contract-test.ps1 的 -Scope/-Quick 参数）不自动开工，另行要求才做。
- .xls 以外的格式（xlsb/ods 等）与超大文件性能压测。
- HTTPS、传输加密、域名备案等既有明确不做项延续。

## Further Notes

- 终检验收口径：contract-test.ps1 三层数量 93/110/16 且 HTTP 未注册=0、额外=0、文档匹配=93；check-docs.ps1 exit 0（编码/围栏、旧词、buf build、RPC 计数、迁移、Status 行）；三服务 go vet 与 go build 全绿。
- 原 P1-1（core vet copylock）已修复并挂到契约脚本，作为本次收口既有的持续防线。
- 事实源流转顺序：接口文档、proto、本规格任一变更，另一侧同批同步；落后者立即表现为 contract 缺口。
- 对应落地票为 01（HTTP 面全量接线与别名治理）、02（.xls 导入解析补全）、03（生命周期到期任务 intake 通道）、04（ForceClose 审计双写去重），均已 ready-for-agent，本规格是其公认验收口径。
