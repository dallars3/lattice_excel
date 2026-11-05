# 02: tracking-ingest（埋点网关端点）

**What to build:** 登录用户可批量上报埋点事件：会话鉴权；逐条校验（event_id 长度、event_name `fe_` 前缀格式、detail 大小）；服务端兜底 user_id/user_type/received_at_ms；Redis 短期幂等去重（故障放行）；限流独立桶（按来源 IP）；校验与盖章后发布 `track.reported`；响应 `{accepted,duplicated,rejected}`；发布失败 503、网关不重试放大；端点自身不产生埋点。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 登录后批上报：响应计数正确，发布事件含全部兜底字段（经内存事件总线断言）
- [ ] 匿名请求返回未登录错误
- [ ] 单条非法事件计 rejected 且不回绝整批；超批/超限整批拒绝
- [ ] 重复 event_id 二次上报：accepted=0、duplicated=1（Redis 故障时放行不误伤）
- [ ] 发布失败返回 503；端点被限速管线独立桶覆盖
- [ ] 表驱动路由测试全绿；文档护栏脚本全绿

## Answer
已落地。`POST /api/v1/tracking/batch`（`excel_web_api/internal/gateway/tracking_routes.go`）：会话鉴权（匿名 2001）；逐条校验（批≤100、event_id 8~64、event_name `^fe_[a-z0-9_]+$`≤64、detail JSON 对象≤4KB 且拒绝尾随垃圾）；Redis SETNX 幂等（TTL 24h，故障放行）；限流独立桶 60/min/IP；逐条盖章 user_id/user_type/received_at_ms；发布 `track.reported` 失败 503 不重试。测试表驱动+内存 eventbus。Evidence: merge `2ff1691`（后续 `550e1ec` 收口 detail 尾随校验与逐条时间戳）。