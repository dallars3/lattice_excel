# 04: ForceClose 审计双写去重

**What to build:** 用户视角：企业管理员强制关闭外部链接后，审计日志对这次动作恰好留痕一条；不因不同消费者都识别链接关闭而出现重复记录。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] 一次 force-close 只产生一条 AUDIT_ACTION_LINK_FORCE_CLOSED 审计记录（无论 link.closed 映射路径还是显式 audit.append 路径）——enqueueAuditAppend 携带 link.closed 的 eventID 作为 audit_event_id，audit 消费者优先以 audit_event_id 作为审计 EventId，两路径归一后靠 uk_excel_audit_events_event_id 幂等只留一条
- [x] 去重依据（event_id 或 reason 指纹）明确写入实现与接口文档审计约定，其他链接关闭场景（close/reset/收窄）行为不变——接口文档 §2.13 增「审计去重约定」、§8.4 E-13 加 audit_event_id?；close/reset/收窄无显式 audit.append 路径，行为不变
- [x] 契约测试覆盖「同一次强关双通道」场景并断言 Query 仅一条——TestForceCloseByManagerProducesSingleAuditRecord（outbox→forwarder→audit 消费者双通道后 Query 断言仅一条）
- [x] 审计相关既有测试与三服务回归全部 exit 0——既有 4 项审计/强关测试保持通过，最终 contract-test 全量回归 exit 0

