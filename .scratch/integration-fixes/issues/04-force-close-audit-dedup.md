# 04: ForceClose 审计双写去重

**What to build:** 用户视角：企业管理员强制关闭外部链接后，审计日志对这次动作恰好留痕一条；不因不同消费者都识别链接关闭而出现重复记录。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 一次 force-close 只产生一条 AUDIT_ACTION_LINK_FORCE_CLOSED 审计记录（无论 link.closed 映射路径还是显式 audit.append 路径）
- [ ] 去重依据（event_id 或 reason 指纹）明确写入实现与接口文档审计约定，其他链接关闭场景（close/reset/收窄）行为不变
- [ ] 契约测试覆盖「同一次强关双通道」场景并断言 Query 仅一条
- [ ] 审计相关既有测试与三服务回归全部 exit 0

