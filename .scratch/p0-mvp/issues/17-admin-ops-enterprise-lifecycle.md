# 17: admin 运营通道与企业注销生命周期

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 02 eventbus-outbox；06 enterprise-members；07 audit；16 file-import-export

**Status:** ready-for-agent

- [ ] 无白名单凭证调用 admin 接口被拒绝；成功动作全部入审计
- [ ] 注册开关即时生效且关闭后注册面行为符合预期
- [ ] 企业注销状态机 none/pending/cooling/revoked/done 正确（30 天可撤回）
- [ ] E-17 幂等消费：附件/导出件/通知全量清理且仅生效一次
