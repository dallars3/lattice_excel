# backend-align map

Status: resolved

## Notes

- 本 effort 覆盖《前后端对齐结论-v0.1》全部后端待改动：基础能力 #1~#3（静态托管/埋点/通知 WS）与契约补全 N1~N8，spec 见 [spec.md](./spec.md)。
- 11 张 tracer-bullet 票按依赖顺序编号；已获批准发布，全部 `ready-for-agent`。
- 护栏口径：P0 票不新增 proto rpc（计数 111 不变）；任何加 rpc 的 P1 备选须同步 check-docs 期望值并走新票。
- 并行注意：04/05/06/07/08/09 均触发 proto 重生成脚本，同批拉取时注意生成目录冲突，建议串行化重生成、并行改代码。
- 测试接缝统一沿用既有：网关表驱动路由测试 + 内存事件总线断言 + core 服务层 harness；不新增接缝。

## Decisions-so-far

- 裁定冻结：N1 排序降级（P0，不显示全量组计数）；N4 视图级宽度；N8 组合实现（后端零改动，仅在 11 号票注明语义）。
- 埋点链路：EventBus 可靠投递异步落库，不新增 gRPC；网关 Redis SETNX 即时去重 + 落库唯一约束双保险。
- 通知推送：复用 worker 已发布 `notify.created`，帧无 seq、不进重放，轮询保留兜底；RedDotObserver 不接线避免双订阅。
- 收口（2026-11-05）：#1~#3 与 N1~N8 全部落地并合入 feature/backend-align。执行期裁定：N6「既有链接解析 rpc」在 proto 111 个 rpc 中不存在，改走注册携带 token 通道（ExchangeSession+principal.LinkContext）保持 RPC=111；W-17 通知帧实际 oneof field=18（17 被 Ack 占用）；护栏终验另修两处既有测试基建（core reset 全表清理、contract-test 期望对齐 v0.7 并补 H-94/H-95 注册清单）。详见各票 Answer。

## Frontier

- 全部 11 张票已 resolved（收口于 feature/backend-align，终验 contract-test 28/28、check-docs fail=0）。