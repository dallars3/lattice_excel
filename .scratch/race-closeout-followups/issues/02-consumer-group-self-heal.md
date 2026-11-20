# 02: Redis 消费组运行期自愈

**What to build:** 可靠消费者在 Redis 读取或 pending 操作中发现消费组缺失时，限频重建消费组并继续消费，无需人工执行 `XGROUP CREATE` 或重启服务。该路径继续遵守现有 event_id 幂等、ack 与 outbox 状态机语义。

**Blocked by:** 01: 测试 Redis 隔离护栏（prefactor）.

**Status:** ready-for-agent

- [ ] 读取 pending、claim 或读取新条目遇到消费组缺失时，消费者可识别并调用 EventBus 的消费组重建接口
- [ ] 重建成功后原消费循环继续运行，后续事件正常交付；不得退出 goroutine、ack 丢弃或把临时错误标记为毒消息
- [ ] 重建失败时限频重试并输出可观测日志，不对 Redis 形成忙循环
- [ ] 重建后恢复消费仍保持重复 event_id 只产生一次业务副作用
- [ ] fake bridge 用例覆盖“首次读取缺失一次，重建后事件送达”与“重建持续失败后限频重试”两条路径
- [ ] 真实隔离 Redis 集成用例可主动删除消费组，随后发布事件并观察消费组重建与正常消费

