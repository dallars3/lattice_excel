# 契约测试覆盖矩阵

本矩阵事实源为[《三服务接口文档 v0.6》](../../接口文档-v0.6.md) §2、§3、§4、§9。执行入口为 `docs/tools/contract-test.ps1`；HTTP 路由来自 `gateway.Pipeline.Routes()` 的运行时快照，gRPC 来自 `proto/**/*.proto` 的 `rpc` 计数，WS 来自生成代码中的 `Frame` payload 变体。

## 三层数量与入口

| 层 | 唯一事实源 | 期望 | 实际断言 | 状态 | 说明 |
|---|---|---:|---|---|---|
| HTTP | §2 H-01..H-93、§8.1、§11 | 93 | `contract-test.ps1` 比对 §2/§8.1/附录并输出运行时路由缺口 | PASS | 数量必须为 93；未注册路由只登记缺口，不伪造覆盖 |
| gRPC | §4、§11、proto RPC | 110 | 脚本比对各服务方法行与 `^\s*rpc ` 计数 | PASS | 复用 `check-docs.ps1` 的 110 口径 |
| WS | §3 W-01..W-16、§8.2 | 16 | 生成 `webapi.pb.go` 的 `isFrame_Payload` 变体为 16，并核对 §3/§8.2 | PASS | hub 的 7 个入站 case 单列展示，不与 16 帧总数混算 |

## 通用检查项 C-01..C-16

| 编号 | 接口或用例 | 断言 | 预期 | 实际 | 状态 | 备注 |
|---|---|---|---|---|---|---|
| C-01 | 全部受保护 HTTP 路由 | `TestProtectedRoutesRejectMissingSessionBeforeAuthorizationOrBusiness`、`TestSessionRequiredRejectsInvalidSessionBeforeBusiness` | 无会话返回 HTTP 401/业务码 2001，RPC 与业务处理均为 0 次 | 已断言状态、业务码和零副作用 | PASS | 管线统一覆盖，不依赖具体业务 handler |
| C-02 | 缺参数/类型错误 | `TestAuthRoutesMapBusinessErrorsToStatusAndEnvelope`、`TestNotifyRoutesRejectMalformedLimitWithoutRPC`、`TestRecordsQueryRejectsMalformedLimitWithoutRPC` | HTTP 400/业务码 1001，message 指明字段 | 状态和 1001 已断言；网关使用通用文案“参数缺失或非法” | GAP | `excel_web_api/internal/gateway/auth_routes.go` 的 `writeCodeError` 丢弃字段定位；所有 handler 需统一结构化字段错误 |
| C-03 | 命名与凭据格式 | `TestRegisterRejectsCaseInsensitiveDuplicateAndInvalidCredentials`、`TestChangePasswordRequiresOldPasswordAndNewPasswordPolicy`、`TestFieldConfigValidationAndPersistence` | 非法用户名、密码、名称和字段配置返回 1001 | 核心服务已逐项断言且失败不落库 | PASS | H-01/H-02/H-05/H-06 与结构命名共用核心校验 |
| C-04 | 角色矩阵外动作 | `TestBaseAuthRejectsDeniedAndExternalExportBeforeBusiness`、`TestStructureLifecycleAndRoleBoundaries`、`TestQueryAndExportOnlyAllowOwnerOrManager` | HTTP/gRPC 返回 3001，业务 RPC 不执行 | 已断言拒绝发生在业务副作用前 | PASS | 网关统一鉴权与核心归属校验双层覆盖 |
| C-05 | 分页 | `TestNotifyRPCsPaginateRetainAndMarkReadIdempotently`、`TestQueryViewAppliesAndFiltersSortsEmptyLastGroupsAndCursor`、`TestEnterpriseQueriesPaginationRejectAndRoleProvider` | `cursor`/`limit`/`has_more`/`next_cursor` 可完整翻页 | 通知、记录、成员分页均有连续页断言 | PASS | 新增分页接口须沿用同一 cursor 契约 |
| C-06 | 写接口幂等 | `TestCreateRecordIsIdempotentForTwentyFourHoursAndForcesCreator`、`TestBatchUpdateReportsExactOutcomesAndReplaysClientRequest`、`TestRedisStreamsDuplicateEventIDIsHandledOnce` | 同 `client_request_id`/`event_id` 重放不新增记录、事件或通知 | 记录、批量写和消费去重已断言 | PASS | HTTP 网关只透传请求 ID，幂等事实在核心/worker 事务内 |
| C-07 | 响应结构 | `TestLoginReturnsEnvelopeAndThirtyDaySIDCookie`、`TestRecordsRoutesUseAuthPipelineEnvelopeAndOneBusinessRPC`、`TestFileDownloadPreservesServerFilenameAndBytes`；另有运行时路由清单 | §8 字段、类型、枚举逐接口一致 | 已接线接口有抽样结构断言；63 个 H 项未注册 | GAP | 未注册 H 项由 `contract-test.ps1` 按文档行号逐项输出；不能宣称 93 个响应已测 |
| C-08 | 容量软上限 | `TestViewConfigLimitsPersistenceNamesAndCapacity`、`TestInitUploadRejectsBlacklistedOversizedAndPoolFullFiles`、`TestFieldConfigValidationAndPersistence` | 业务容量返回 5001，文件容量返回 5002，文案明确 | 视图、字段、文件和附件池已有断言；评论 2000 字与记录 5 万行无契约级用例 | GAP | 评论写 handler 仍为空壳 `excel_web_api/internal/gateway/create_comment.go`；记录上限实现见 `excel_core_service/internal/records/batch_update.go`，待补边界测试 |
| C-09 | 敏感入口限速 | `TestPipelineRateLimitsSensitiveRoutesWith5003`、`TestPipelineDoesNotRateLimitUnrelatedRoutes`、`TestRateLimiterSlidingWindowRejectsExcessRequests` | 注册 10/min、登录 20/min、记录写与评论写 120/min，按 IP 超频返回 HTTP 429/5003 | Redis 滑动窗口与四类路由接入已断言；无关路由零调用 | PASS | 失败定位同时覆盖 `Retry-After` 与鉴权 RPC 前置拦截 |
| C-10 | 错误码到 HTTP 状态 | `TestErrorCatalogContract`、`TestAuthRoutesMapBusinessErrorsToStatusAndEnvelope`、`TestRecordsWriteRoutesExposeConflictData` | 1001/2001/3001/3002/4001/4002/5001/5002/5003/9001 映射固定 | 三服务错误目录与网关信封均有逐值断言 | PASS | 5003 固定映射为 429 |
| C-11 | 审计副作用 | `TestEightEventTopicsAreMappedAndPersistedThroughOutbox`、`TestQueryAndExportDefaultToRecent180Days`、`TestAuditPersistenceHasNoUpdateOrDeleteStatements` | 动作后 `audit.Query` 可见、不可改删、默认 180 天 | 事件投影、查询、保留期与不可变语句均已断言 | PASS | 审计导出自身也写入数据导出动作 |
| C-12 | 事件副作用 | `TestOutboxPublishesOnlyAfterBusinessTransactionCommits`、`TestRedisStreamsDuplicateEventIDIsHandledOnce`、`TestEnterpriseMemberEventsAreObservableOnMemoryBus` | 事件头齐全、type/version 正确、重复 event_id 消费一次 | 事务提交、头字段、公共事件与消费去重均有断言 | PASS | 具体业务事件按其现有专项测试覆盖 |
| C-13 | 会话回收 | `TestLinkClosedRevokesEveryRoomInBaseImmediately`、`TestAuthorizationCacheTTLAndLinkClosedInvalidation` | 链接关闭/收窄后目标房间收到 W-15，且不超过 60 秒 | WebSocket 即时推送与 55 秒缓存失效已断言 | PASS | 无关 Base 不收到吊销帧也有断言 |
| C-14 | 外部禁导出 | `TestExportRouteRejectsExternalCollaboratorBeforeBusinessRPC`、`TestExternalAndAnyoneAuthorizationCapsCannotBeBypassed` | 外部账号导出返回 3002，且业务 RPC 不执行 | HTTP 3002 与核心授权上限均已断言 | PASS | 即使外部角色为 editor 也必须拒绝 |
| C-15 | 行级可见性 | `TestRecordsReadsApplyRowScopeToRecordsRelationsAndLookups`、`TestUpdateCellRowScopeOwnRejectsAnotherCreatorsRecord`、`TestGetHistoryUsesNinetyDayWindowFieldValuesAndRowScope` | filler 仅见本人记录，关联与 Lookup 滤除不可见目标 | 读、写、历史及关联/Lookup 均已断言 | PASS | 行范围由 share 注入并由 records 强制过滤 |
| C-16 | admin RPC | `TestAdminOperationsRejectMissingToken`、`TestAdminRegisterSwitchPersistsImmediatelyAndAudits`、`TestAdminEmergencyActionsTakeEffectAndAudit` | 无白名单凭证拒绝；成功动作全部入审计 | 鉴权拒绝、即时生效和审计可见均已断言 | PASS | admin 不经公网 HTTP，不进入网关限速 |

## 运行口径

- 三服务按 `core -> worker -> web` 顺序串行执行 `go test -p 1 ./... -count=1`，避免共享 core 表跨服务竞争。
- 脚本先执行 §2/§8.1/§11 与 proto 的数量一致性检查；任一数量不一致立即 FAIL 并带文档行号。
- 脚本随后运行 HTTP `Routes()` 清单、六个 C 项过滤抽查、矩阵结构校验和三服务全量包测试。
- `GAP` 表示已知未闭环项，会进入缺口清单但不掩盖已有测试失败；测试失败或事实源数量不一致才使进程 exit 1。
