# 05: web_api 统一鉴权管线与直通模板

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 03 share-authz；04 iam-auth

**Status:** done

- [x] 未登录访问受保护接口返回 2001/401，且不触达业务 RPC
- [x] 越权 3001 与外部导出 3002 在网关拦截（转发前断言）
- [x] 直通接口仅一次 RPC 换壳并返回统一信封；Redis 故障时 fail-open 放行并告警
- [x] 鉴权结果缓存 TTL<60s 且事件失效可即时清缓存

## Comments

- 2026-11-05 code-review 补注：第 3 条的 fail-open 语义明确覆盖鉴权缓存与限速器两处 Redis 依赖——限速器故障同样放行（可研性优先于治理强限），实现于 gateway pipeline（ERROR 日志 + fail_open_total 累计计数）。与 story 55 "enforce rate limits" 的张力经评审裁定为接受：单机自托管下 Redis 故障 ≈ 整机故障，fail-closed 只会把基础设施问题放大为全站 5xx。
