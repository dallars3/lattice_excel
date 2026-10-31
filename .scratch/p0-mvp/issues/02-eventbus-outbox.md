# 02: EventBus 与 outbox 基座

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 01 bootstrap

**Status:** ready-for-agent

- [ ] 业务写事务内同库落 outbox，事务提交后事件可被发布（契约测试可观察）
- [ ] 同一 event_id 重复投递，消费者仅生效一次
- [ ] 消费失败指数退避重试，超限进死信且死信可查
- [ ] PubSub/Streams 与可切换 Kafka 共用同一 EventBus 接口（切换不改业务代码）
