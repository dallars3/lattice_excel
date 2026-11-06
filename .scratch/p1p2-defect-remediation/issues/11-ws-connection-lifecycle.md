# 11: WS 连接生命周期

**What to build:** WebSocket 握手校验会话、Base/表存在性与访问权；非法请求在升级前被拒且不挂死；连接关闭或离开房间时向房间广播 presence 离场；重复关闭幂等。对应缺陷 #18（握手与 presence 部分）。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 不存在/无权的 Base 握手被拒（403/404 语义），不会 101 升级。
- [ ] 双连接同表，一方关闭后另一方在 5 秒内收到 `presence.online=false`。
- [ ] 重复关闭不重复广播、不报错。
- [ ] 已修复的 `session_revoked` 先发帧后关连接语义不回归。
