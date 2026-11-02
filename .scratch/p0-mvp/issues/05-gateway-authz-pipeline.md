# 05: web_api 统一鉴权管线与直通模板

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 03 share-authz；04 iam-auth

**Status:** done

- [x] 未登录访问受保护接口返回 2001/401，且不触达业务 RPC
- [x] 越权 3001 与外部导出 3002 在网关拦截（转发前断言）
- [x] 直通接口仅一次 RPC 换壳并返回统一信封；Redis 故障时 fail-open 放行并告警
- [x] 鉴权结果缓存 TTL<60s 且事件失效可即时清缓存
