# 04: httpClient 层（信封/ApiError/13 错误码目录/幂等/游标，MSW 配对）

**What to build:** 全部后端交互的通信底座：统一解析 `{code,message,data}` 信封、抛出类型化 ApiError；13 个错误码落成「文案/动作/恢复策略」目录（单一事实源）；写操作自动携带幂等 client_request_id 且重试复用；游标分页 helper 与 useInfiniteQuery 适配走通；MSW 配置就位可拦截接口并返回任意错误码，供本票与 09 复用。

**Blocked by:** 01: 工程底座

**Status:** ready-for-agent

- [ ] 信封解析：`code===0` 透传 data；非 0 抛出含 code/message 的类型化 ApiError
- [ ] error-catalog 覆盖全部 13 个错误码（1001/1002/2001/2002/3001/3002/4001/4002/5001/5002/5003/9001/9002），每条含文案/动作/恢复策略三元组
- [ ] error-catalog 表驱动测试逐码断言三元组完整
- [ ] 写请求自动生成并携带 client_request_id，重试复用同一 id（可测）
- [ ] 游标 helper 适配 useInfiniteQuery（cursor/limit/has_more/next_cursor），集成用例走通
- [ ] MSW 配置就位：可对任一后端路由注册 handler 并返回指定错误码（供 09 复用）