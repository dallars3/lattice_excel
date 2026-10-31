# 14: comment + notify

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 02 eventbus-outbox；05 gateway-authz-pipeline；12 records-write

**Status:** ready-for-agent

- [ ] 两级评论/回复/@ 解析/解决状态与未解决计数正确
- [ ] 通知矩阵各事件逐类产生正确通知且跳转定位可达
- [ ] 未读计数与单条/全部已读幂等正确
- [ ] 在线红点经 notify.created 推送至网关可见
