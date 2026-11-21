# 07: live stack 全量回归收口

**What to build:** 在 live stack 运行状态下完成本批全部验收门，证明测试 Redis 已安全隔离、消费组可恢复、P0 网格基线稳定、三个下拉编辑器用例真实通过，且视图配置与记录查询链路不再出现 120 秒级停滞。

**Blocked by:** 02: Redis 消费组运行期自愈; 04: 下拉编辑器 E2E 语义收口; 06: 网格停滞 trace 与重复稳定性收口.

**Status:** done

- [x] Go 核心与 worker 全量测试在 live stack 运行时通过，测试前后现有 Redis Streams 消费组保持可消费
- [x] 消费组缺失模拟用例通过，calc/notify/tracking/audit 等消费者可在限频窗口内自动恢复并处理后续事件
- [x] sf-stress 六条用例通过，视图配置保存、旧响应隔离和 Record 查询超时错误态相关专项用例全绿
- [x] P0 全量通过，TC-050-01 连续执行 20 次零失败，三个新增入口均完成 H-48 服务端与 UI 双数量断言
- [x] p1p2 全量 54 条通过、0 失败；TC-040-07/09/14 不再跳过，多选、人员和关联提交后均完成落库验证
- [x] 网格停滞专项完成要求的重复运行，无 120 秒级停滞，失败 trace 机制可用
- [x] 文档守则以退出码 0 通过；除明确记录的既有非本批问题外无其他回归

## Comments

### 2026-11-21 live stack 全量收口

**环境：** 07 源码只读构建副本使用隔离 Redis `127.0.0.1:6380`、core `9191`、worker `9192`、gateway `8091`；未用共享 `6379` 执行清理或替换 live stack。

**Redis 与 Go 门禁：**

- core `go test -p 1 -count=1 ./...`：exit 0，15 包通过；worker 同命令 exit 0，全部包通过。
- Go 全量测试前后隔离 Redis：Streams `19 -> 19`、groups `25 -> 25`、pending `0 -> 0`；共享 Redis：Streams `21 -> 21`、groups `25 -> 25`、pending `0 -> 0`。
- 证据：`rf07-build\out\live-stack\evidence\go-core.stdout.log`、`go-worker.stdout.log`、`redis-before-go-tests.json`、`redis-after-go-tests.json`、`redis-go-test-comparison.json`。

**消费组运行期自愈：**

- 依次删除隔离 Redis 的 `calc.job/core-calc`、`track.reported/core-tracking`、`comment.mentioned/worker-notify`、`record.deleted/core-audit`，每次删除后均观察到组不存在。
- 通过真实 8091 API 分别触发公式重算、埋点批量上报、本人 mention 和记录删除；四个消费组均自动重建，pending 均为 0，last-delivered 均追到对应 stream 尾。
- 业务落库：公式结果读回 `20`；tracking API 受理后消费成功；`comment.mentioned` 通知读回且 comment_id 匹配；`AUDIT_ACTION_RECORD_DELETED` 审计读回且 target 匹配记录 ID。
- 证据：`rf07-build\out\live-stack\evidence\consumer-self-heal-live.json`。

**Playwright live stack：**

- sf-stress：**6 passed / 0 failed**。
- 网格停滞基线：**5 passed / 0 failed**；`--repeat-each=7`：**35 passed / 0 failed (1.2m)**，无 120 秒级停滞；spec 使用 `trace: retain-on-failure` 与 grid-stall diagnostics。
- full P0：**32 passed / 0 failed**；TC-050-01 使用 `P0_TC05001_REPEAT=20` 单独执行：**20 passed / 0 failed**。三入口服务端数量与 UI 计数双断言均通过。
- full p1p2：**54 passed / 0 failed (1.6m)**；TC-040-07/09/14 全部实际执行，多选、人员、关联均完成 API 读回落库验证。
- p1p2 首轮曾因沿用错误目标变量而连接未运行的 8080，失败发生在 beforeAll；改用 `P1P2_BASE=http://127.0.0.1:8091` 后重跑。P0 首轮另有一次跨套件注册限流 `5003`，等待配置的 `10/min` 窗口自然过期后，不改限流键重跑为 32/32。

**本轮消除的顺序依赖：**

- TC-040-07 最后移除选项后等待编辑器收起及 API 读回，避免 TC-040-08 新页面读到旧版本并触发 4001 并发冲突。
- TC-050-16 显式设置 record B 关联值并使用 record B 自身版本；全字段搜索按真实语义断言 A/B 双命中及两处高亮。
- TC-180-03 在埋点用例前清空视图筛选/排序/分组/隐藏列，避免前序视图配置使“数量”列不可达。

**组件与构建：**

- `editors.test.tsx`：**41/41 passed**。
- `BaseWorkspacePage.views.test.tsx`：**29/29 passed**，包含票 05 的 H-47/H-48 时序、旧响应隔离、15 秒超时与取消透传测试。
- `npm run build`：TypeScript noEmit 与 Vite production build 通过。

**文档守则：** 收口文档修改后执行 `powershell.exe -NoProfile -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1`：退出码 0，检查 209 个 Markdown 文件，`fail=0`。
