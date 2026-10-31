# 07: 审计模块

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 02 eventbus-outbox

**Status:** ready-for-agent

- [ ] Append 按 event_id 幂等；audit 记录不可更新/删除
- [ ] 180 天保留语义存在（清理任务由后续票接入）
- [ ] 仅企业 owner/manager 可查询与导出 CSV，其余身份 3001
- [ ] 导出/导入/记录删除/链接强关/成员停启用/所有者转移等动作留痕可追溯
