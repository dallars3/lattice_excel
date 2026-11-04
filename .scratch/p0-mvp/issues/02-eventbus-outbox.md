# 02: EventBus 与 outbox 基座

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 01 bootstrap

**Status:** done

- [x] 业务写事务内同库落 outbox，事务提交后事件可被发布（契约测试可观察）
- [x] 同一 event_id 重复投递，消费者仅生效一次
- [x] 消费失败指数退避重试，超限进死信且死信可查
- [x] 总线接口可替换：EventBus 接口抽象 + 内存/Redis（PubSub/Streams）两实现，切换不改业务代码（Kafka 适配器留待多机部署阶段，见 docs/adr/0001；code-review 2026-11-05 裁定口径）
