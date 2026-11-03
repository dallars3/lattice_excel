# 13: realtime 实时协作

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 02 eventbus-outbox；05 gateway-authz-pipeline；12 records-write

**Status:** done

- [x] join 返回 snapshot；断线重连按 last_seq 补发无丢无重
- [x] 他人修改 ≤1s 广播到同房客户端；50ms 合帧生效
- [x] presence/光标/「编辑中」状态与内外部标签正确
- [x] 链接关闭后 1 分钟内广播 session_revoked
