# 编码标准（全仓库统一）

> 本文件将本仓库已确认的**命名、文件结构、代码编写、架构风格与接口契约约束**收口为一份标准。实现阶段以 AGENTS.md 的文档护栏指针触发；评审阶段（review/retro 及未来 code-review 流程）按本文件逐条核对。与三份服务方案、接口文档表述冲突时，以本文件为准。

## 1. 命名基线

| 类别 | 约束 |
|---|---|
| 词法 | URL 段 kebab-case（`read-all`/`open-links`）；JSON/gRPC 字段 snake_case；枚举值 snake_case（proto 用 UPPER_SNAKE，首值 `*_UNSPECIFIED`） |
| 身份术语 | `admin`=运营者；`manager`=企业管理员与 Base「可管理」；`member`=普通成员；`operator`=操作发起人；`author`=评论作者；`created_by`=创建人 |
| 链接上下文 | 统一 `LinkContext`（字段 `link_context`），弃用 `LinkCtx/link_ctx` |
| 数组字段 | 以 `_list` 结尾（`record_list`、`field_list`） |
| kv/映射字段 | 以 `_map` 结尾（`cells_map`、`config_map`、`payload_map`、`detail_map`、`schema_map`、`row_context_map`） |
| ID 与路径占位 | ID 统一 `<资源>_id`；单资源 `{id}`、多资源 `{<资源>_id}` |
| 时间字段 | `created_at`/`updated_at`（记录）、`occurred_at`（事件）、`deadline_ms`（截止）；统一毫秒时间戳 |
| 服务命名 | HTTP `excel_xxx_api`；RPC `excel_xxx_service`；模块 proto 包 `excel.{module}.v1`；`{Method}Request/{Method}Response`；公共消息归 common |
| 数据表前缀 | 统一 `excel_<模块名>_`（如 `excel_iam_`）；业务主键 ULID 字符串，append-only/队列/seq 表允许 bigint 自增 |

## 2. 文件结构规则

1. **模块文件化**：除通用逻辑外，不同模块代码分隔在不同文件，文件名以模块名命名（如 `rpc/iam.go`、`base/records.go`）。
2. **可复用归口**：各包内可复用逻辑集中在 `common.go`；`doc.go` 仅作包说明。
3. **配置命名**：统一 `<服务>_config`（`web_api_config.yaml`、`core_service_config.yaml`、`worker_service_config.yaml`）。
4. **internal 一文件一接口**：`internal/` 以模块名建文件夹（`internal/iam/`），文件以接口名命名，一个文件一个接口（`internal/iam/register.go`）；`common.go` 为例外。
5. **base 层与表所有权**：`base/` 按模块分文件；A 模块代码只操作 A 模块表；跨模块数据在代码逻辑层拆分/聚合，确需跨模块 SQL 才允许且必须注释原因。

## 3. 代码编写规则

- 函数名必须体现其主要作用；一个函数保持单一功能性。
- 单函数尽量 ≤500 行；预计超限必须先寻找拆分方案（步骤/子任务/收窄接口），经评估确不便拆分方可超限。
- 接口实现保持“薄适配 + 业务 + 数据”直线：业务判断不得超过分层职责；一行能说完的转换不留中间态。
- 错误一律走契约错误码，禁止静默吞错；日志不落密码/会话票据/`Authorization`/`link_token`/请求正文/附件内容。

## 4. 架构风格

### 4.1 三进程边界

- `excel_web_api`：唯一公网面，**零 MySQL**；只做身份识别、统一鉴权（`share.CheckAccess`）、协议转换、最小聚合；业务规则/状态机/事务全部下沉。
- `excel_core_service`：业务主域（8 模块）；单一数据所有权；`CheckAccess` 是唯一角色裁决口，输出 `{allow, role, internal, row_scope}`；业务模块只做行级过滤，不重复判角色。
- `excel_worker_service`：异步 IO 域（4 模块）；不重复角色校验，仅归属校验；任务与文件流皆在本域。
- admin 接口只走 localhost RPC（脚本调用），一律不暴露 HTTP 且入审计。

### 4.2 数据与可靠性

- 写事务内同库追加 outbox；事件头 `event_id/type/version/base_id/seq/occurred_at/producer`，消费者按 `event_id` 幂等。
- Redis 只承载可丢弃数据（限速/短缓存/PubSub/Streams）；可靠性由 MySQL outbox 保证；Kafka 经 EventBus 统一接口可选切换。
- 会话/鉴权缓存 TTL<60s，配合事件主动失效实现链接熔断 1 分钟内全局生效。
- 记录写采用乐观锁：版本不符返回 4001 并附对方新值，禁止静默覆盖；批量/导入等写带 `client_request_id` 幂等。

### 4.3 计算与实时

- 公式/引用/Lookup 后端权威物化：同行公式写事务内同步求值，跨行依赖经 `calc.job` 异步拓扑重算。
- WebSocket 房间纯内存：在线/光标/编辑中不落库，断线按 `last_seq` 对账补发；广播 ≤1s、50ms 合帧；优雅退出推送 `session_revoked`（close 1001）。

### 4.4 配置与运维

- 权限角色→动作矩阵放独立配置仓库 `excel_permission_config`（JSON，启动加载+热更）；动态授权（协作者/链接）存 MySQL。
- 端口：8080 公网唯一；core 9091、worker 9092、MySQL/Redis 仅 localhost。

## 5. 接口契约约定

- 统一响应 `{code, message, data}`；错误码映射固定：`1001→400`、`2001→401`、`3001/3002→403`、`4001/4002→409`、`5001/5002→413`、`5003→429`、`9001→5xx`。
- 幂等符号：`RW` 只读天然幂等；`NAT` 自然键幂等；`EID` event_id；`CRI` client_request_id；`TOK` 会话级。
- 异常策略符号：`F` 明确错误码；`E` 空/默认兜底；`P` 局部降级；`R` 重试+死信；`KEEP` 失败保留用户输入。
- 接口分级：`P0` 主链路/底层依赖；`P1` 旁支业务；`P2` 低频/非对外/异步内部。
- 依赖简写：`RPC:模块.方法`；`DB:表`（省略 `excel_<模块>_` 前缀）；`RED:key用途`；`CFG:文件`；`BUS:事件名`；`DISK:目录`。

## 6. 工程流程

- 文档（.md/.proto/.yaml/配置）改动后必跑 `docs\tools\check-docs.ps1`（退出码 0），Go 改动必跑三模块 `go build ./...`；`git commit` 会被 pre-commit 钩子强制复查文档护栏。
- 事实源同步纪律：接口文档、proto、三份服务方案、PRD 任一变更，须同步受影响文档；破坏性 proto 变更升 v2，HTTP 冻结 `/api/v1`，proto 字段只增不改、删除用 `reserved`。
- 第三方依赖：仅宽松许可（MIT/BSD/Apache-2.0）；禁 AGPL/GPL/商业表格内核与公式引擎；前端组件同等核验，不接第三方 CDN/统计/字体/上报 SDK。

## 附：参考索引（指针，规则细节在对应文档）

- 需求基线：`协作多维表格（代号）-产品需求文档-v1.3.md`
- 总体架构：`整体技术方案-v1.3.md`
- 服务设计：`excel_web_api-技术方案-v0.1.md`、`excel_core_service-技术方案-v0.1.md`、`excel_worker_service-技术方案-v0.1.md`
- 接口清单与参数/校验/网合：`接口文档-v0.6.md`
- 契约：`proto/`（webapi.v1 + common + 12 服务包）
- 护栏：`docs\tools\check-docs.ps1`；分工：`.scratch/p0-mvp/spec.md` 与 `issues/`
