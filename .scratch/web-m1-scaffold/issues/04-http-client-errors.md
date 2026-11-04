# 04: httpClient 层（信封/ApiError/13 错误码目录/幂等/游标，MSW 配对）

**What to build:** 全部后端交互的通信底座：统一解析 `{code,message,data}` 信封、抛出类型化 ApiError；13 个错误码落成「文案/动作/恢复策略」目录（单一事实源）；写操作自动携带幂等 client_request_id 且重试复用；游标分页 helper 与 useInfiniteQuery 适配走通；MSW 配置就位可拦截接口并返回任意错误码，供本票与 09 复用。

**Blocked by:** 01: 工程底座

**Status:** ready-for-human

- [x] 信封解析：`code===0` 透传 data；非 0 抛出含 code/message 的类型化 ApiError
- [x] error-catalog 覆盖全部 13 个错误码（1001/1002/2001/2002/3001/3002/4001/4002/5001/5002/5003/9001/9002），每条含文案/动作/恢复策略三元组
- [x] error-catalog 表驱动测试逐码断言三元组完整
- [x] 写请求自动生成并携带 client_request_id，重试复用同一 id（可测）
- [x] 游标 helper 适配 useInfiniteQuery（cursor/limit/has_more/next_cursor），集成用例走通
- [x] MSW 配置就位：可对任一后端路由注册 handler 并返回指定错误码（供 09 复用）

## Comments

- feat commit：`1592686`（分支 feature/m1-web）。
- 落点：`web/src/core/http/{client,error-catalog,idempotency,cursor}.ts` + 同目录 4 个测试；`web/src/query-keys/records.ts`（`['records', viewId]` 固定 key 与 `['records', viewId, cursor]` 单页 key 双形状，plan §3.5）；`web/src/mocks/{server,handlers}.ts`（`mockEnvelope({path, code})` 任意路由返回指定错误码、`mockCursorRecords` 真实游标语义假接口，供 09 复用）；`web/src/test/setup.ts` 接入 `vite.config.ts` 的 `test.setupFiles`（MSW 生命周期 + `onUnhandledRequest: 'error'`）。
- 契约要点：ApiError.meta 承载信封 data（4001 → their_value/their_version 直达冲突弹窗）；层级按技术方案 5.1 原文（全局 2001/3001/5003/9001/9002，局部 1001/1002/2002/4001/4002/5001/5002/3002）；恢复策略枚举 relogin/reinput/readonly-fallback/resolve-conflict/refresh/retry/none，注释写明处理语义；非信封/网络失败合成 9001 兜底。
- 验证：`npm run test` 186/186 绿（基线 126 不回归，新增 60）；`npm run build` 成功，JS chunk 4 ≤6；`dist/assets` grep 确认 msw 未进产物；`docs/tools/check-docs.ps1` fail=0。
