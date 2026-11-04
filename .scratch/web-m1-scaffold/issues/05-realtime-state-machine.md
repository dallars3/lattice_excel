# 05: realtimeClient 状态机（transport 注入 + Fake）

**What to build:** 无真后端即可完整验证的实时通道状态机：`idle → connecting → joined(snapshot) → reconnecting（指数退避，恢复后 replay_request 带 last_seq）→ revoked`；心跳 15 秒；W 系列消息类型化并分发为可订阅事件；传输层以接口注入（生产实现占位、测试用 Fake）。M3 接入真 WS 时只替换传输实现，状态机与消费方零改动。

**Blocked by:** 01: 工程底座

**Status:** ready-for-agent

- [ ] 五态迁移表驱动测试绿（含断线退避 1s 起、重连成功回 joined、revoked 不可逆）
- [ ] 心跳到期/应答行为可测（定时器可注入，测试不依赖真实等待）
- [ ] W 消息类型定义覆盖 W-01~W-16 全部消息名（可枚举断言），订阅分发可测
- [ ] transport 接口注入：Fake 与生产占位并存，全部测试不触真实网络