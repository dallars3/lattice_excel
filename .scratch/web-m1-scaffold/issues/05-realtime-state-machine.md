# 05: realtimeClient 状态机（transport 注入 + Fake）

**What to build:** 无真后端即可完整验证的实时通道状态机：`idle → connecting → joined(snapshot) → reconnecting（指数退避，恢复后 replay_request 带 last_seq）→ revoked`；心跳 15 秒；W 系列消息类型化并分发为可订阅事件；传输层以接口注入（生产实现占位、测试用 Fake）。M3 接入真 WS 时只替换传输实现，状态机与消费方零改动。

**Blocked by:** 01: 工程底座

**Status:** ready-for-human

- [x] 五态迁移表驱动测试绿（含断线退避 1s 起、重连成功回 joined、revoked 不可逆）
- [x] 心跳到期/应答行为可测（定时器可注入，测试不依赖真实等待）
- [x] W 消息类型定义覆盖 W-01~W-16 全部消息名（可枚举断言），订阅分发可测
- [x] transport 接口注入：Fake 与生产占位并存，全部测试不触真实网络

## Comments

- feat commit：`0f339b5`（分支 feature/m1-web）。
- 落点：`web/src/core/realtime/{messages,scheduler,heartbeat,transport,fake-transport,state-machine,index}.ts` + 同目录 4 个测试（messages/heartbeat/transport/state-machine， Vitest 表驱动）；`web/src/test/manual-clock.ts` 手控时钟（Scheduler 注入面）。plan §4 落点四文件之外补 `scheduler.ts`（状态机与心跳共享的定时器接缝）与 `fake-transport.ts`（测试 Fake 与生产占位分文件，不混进门面）。
- 状态机：五态迁移表驱动 9 用例（idle→connecting→joined；connecting/joined/reconnecting 断线均入 reconnecting；reconnecting 重试成功回 joined、失败退避续增；joined/reconnecting → revoked）；revoked 终态不可逆（connect/开闭/消息/disconnect 全部短路、退避与心跳定时器清零、零帧发出）；重连成功回 joined 后发 `replay_request`（last_seq=本地 seq 高水位、单调取 max；首次接入无历史不发）；退避序列 1,2,4,8,16,30,30s 注入手控时钟验证、30s 封顶、回 joined 重置回 1s。
- 心跳：`HeartbeatMonitor` 15s（`HEARTBEAT_INTERVAL_MS=15000` 常量断言）；到期发 heartbeat（W-03）、ack（W-16）重置超时判定、整周期无应答判死转 reconnecting；仅 joined 态运行，断线/吊销即停。全部时间行为经注入 Scheduler，测试无真实等待。
- 消息：W-01~W-16 名称/方向/负载逐一抄录《接口文档-v0.6.md》8.2（16 条可枚举断言一个不少一个不多；C→S 7 + S→C 9 与方向划分一致）；`seq`/`last_seq` 对账字段 `expectTypeOf` 类型断言；range/cell value/change_type 接口文档未细化处落透传类型（不新造契约字段）；`parseServerMessage`/`parseClientMessage` 非法帧（非 JSON/未知 t/方向不符）返回 null 不抛异常。
- 传输注入：`Transport{connect,send,close}` + `TransportHandlers` 回调经 `TransportFactory` 注入，状态机仅依赖接口；`FakeTransport` 脚本化 `serverOpen/serverMessage/serverClose/failConnect` + `sent/sentMessages` 对账断言；`WsTransport` 生产占位（浏览器 WebSocket，类型完整可构造，stub WebSocket 验证接线，不触真实网络）。架构验收点：M3 换真传输只改 factory 实现（WsTransport 接 env.WS_URL 装配），状态机与消费方零改动。
- 验证：`npm run test` 258/258 绿（基线 186 不回归，新增 72）；`npm run build` 成功，JS chunk 4 ≤6（realtime 未进演示页构建图，M3 store 接线后按既有 manualChunks 归组）；`docs/tools/check-docs.ps1` fail=0。
