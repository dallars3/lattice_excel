# 02: Redis 消费组运行期自愈

**What to build:** 可靠消费者在 Redis 读取或 pending 操作中发现消费组缺失时，限频重建消费组并继续消费，无需人工执行 `XGROUP CREATE` 或重启服务。该路径继续遵守现有 event_id 幂等、ack 与 outbox 状态机语义。

**Blocked by:** 01: 测试 Redis 隔离护栏（prefactor）.

**Status:** done

- [x] 读取 pending、claim 或读取新条目遇到消费组缺失时，消费者可识别并调用 EventBus 的消费组重建接口
- [x] 重建成功后原消费循环继续运行，后续事件正常交付；不得退出 goroutine、ack 丢弃或把临时错误标记为毒消息
- [x] 重建失败时限频重试并输出可观测日志，不对 Redis 形成忙循环
- [x] 重建后恢复消费仍保持重复 event_id 只产生一次业务副作用
- [x] fake bridge 用例覆盖“首次读取缺失一次，重建后事件送达”与“重建持续失败后限频重试”两条路径
- [x] 真实隔离 Redis 集成用例可主动删除消费组，随后发布事件并观察消费组重建与正常消费

## Comments

**实现记录（2026-11-20）**

- 分支 `fix/rf-02-eventbus-self-heal`；实现与测试提交 `b4be8b7`；集成基线 `7e7df7a`（含票 01 `excel-libs/testredis`）。
- `RedisSubscription` 的现有 receive cycle 现在识别 `ListPending`、`Claim`、`ReadGroup` 返回的 `NOGROUP`，调用同一 `RedisBridge.EnsureGroup` 后重跑当前读取步骤。恢复过程不退出 goroutine、不生成 Delivery、不调用 `Ack`，临时 Redis 错误和重建失败均保持为上游可见错误。
- 重建失败按订阅级状态限频：首次后 1s 重试，指数退避至 1 分钟上限；成功立即清零。失败通过 `slog.Default` 记录 `stream`、`group`、`topic`、`consumer`、`attempt`、`retry_in` 与 Redis `error` 上下文。
- 未修改 Event、EventBus、Subscribe/Ack 契约，未触碰业务处理器、outbox 状态机或 event_id 幂等逻辑。既有 Bridge 的 `EnsureGroup(..., "0")` 语义保留，因此消费组元数据丢失后的补偿读取仍由 event_id 幂等兜底。
- fake `RedisBridge` 覆盖三条识别路径：`ListPending` 后恢复并送达事件；`ListPending` 成功后组在 `Claim` 前消失；`ReadGroup` 直接返回一次 `NOGROUP`。失败路径覆盖持续 `EnsureGroup` 错误、限频重试与日志字段。
- 真实集成测试通过 `testredis.Open(..., "127.0.0.1:6379")` 固定使用隔离 DB 1；建立流/组后执行 `XGroupDestroy`，再发布事件，同一订阅成功重建组、交付并 `Ack` 该事件。

**TDD 与验证证据**

- 首个 fake red：`cd libs; go test ./eventbus -run TestReliableSubscriptionRecoversMissingGroupDuringPendingRead -count=1` 失败于 `list pending stream entries: NOGROUP ...`；实现最小恢复路径后同一命令 `ok excel-libs/eventbus`。
- 限频 red：在启用恢复但移除冷却判断的只读 overlay 上重放同一 fake 用例，`EnsureGroup retry interval = 54.1135ms, want rate-limited delay`；加入订阅级指数退避后最终套件该用例通过，第二次重建尝试发生在至少 750ms 后且无忙循环。
- 真实 Redis red/green：以 `HEAD:libs/eventbus/redis.go` 只读 overlay 重放删除组用例，失败于 `list pending stream entries: NOGROUP ...`；当前实现下 `TestRedisBusRecoversDeletedConsumerGroup` 通过（隔离 DB 1，实时重建、交付、ack）。
- eventbus 全套：`cd libs; go test ./eventbus -count=1` 通过（5 个用例）。
- 既有语义回归：core `go test -p 1 ./bus -count=1` 通过（含 outbox/幂等/重试/空闲续轮询/外部事件）；worker `go test -p 1 ./bus -count=1` 通过（含重复 event_id 只处理一次）；core `go test -p 1 ./redis -count=1` 通过。
- 构建：libs、core、worker、web_api 的 `go build ./...` 均通过。

