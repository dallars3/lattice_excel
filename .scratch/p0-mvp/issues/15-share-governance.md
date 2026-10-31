# 15: share 分享治理

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 06 enterprise-members；09 workspace-bases；13 realtime

**Status:** ready-for-agent

- [ ] 外部协作者显式上限 editor、manager 拒绝；不可移除/降级自己且至少 1 名 manager
- [ ] 「任何人」链接仅 viewer/filler；收窄或关闭后旧会话 ≤60s 全部失效
- [ ] 重置链接旧链立即作废；重开沿用原 URL
- [ ] 企业 manager 强制关闭外链并通知创建者、进入审计
