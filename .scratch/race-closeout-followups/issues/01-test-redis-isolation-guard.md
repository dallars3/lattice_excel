# 01: 测试 Redis 隔离护栏（prefactor）

**What to build:** 为核心与 worker 的 Go 测试提供统一、默认安全的 Redis 隔离入口。测试只能使用专用逻辑库或独立 Redis 实例；任何清理动作在连接或执行前校验目标，发现仍指向 live stack 时直接失败。完成后，在 live stack 运行状态下执行全量 Go 测试不会删除现有 Streams 消费组或影响业务消费链路。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 测试 Redis 入口支持独立的逻辑库或专用实例，默认目标与 live stack 不同；受限部署可经显式配置切换到专用测试实例
- [x] 不安全的共享目标在任何清理动作前硬失败，报错明确指出测试 Redis 与 live stack 未隔离；不得静默回退到共享库
- [x] 核心与 worker 测试中的 session、登录锁、Streams、EventBus 等辅助客户端统一通过隔离入口创建，测试代码不再直接清理共享 Redis
- [x] live stack 运行状态下执行核心与 worker 全量测试，现有 Streams 消费组与 pending 状态前后保持可消费，calc/notify/tracking/audit 等消费者未停摆
- [x] 既有 Go 测试全部保持绿，隔离入口的正反路径有自动化覆盖

## Comments

**实现记录（2026-11-20）**

- 分支 `fix/rf-01-redis-isolation`；实现提交 `9c0badd`（14 文件，+317/−50）；集成合并提交 `14ee687`，包含 `4f9c2d1` 的 `.codex-agent-*` 文档守卫排除。
- 新增 `excel-libs/testredis` 统一入口：默认复用 live stack 的 `redis_addr` 但使用 DB 1；通过 `EXCEL_TEST_REDIS_ADDR` / `EXCEL_TEST_REDIS_DB` 可显式切换到专用测试实例或逻辑库。`Resolve`、`Open`、`FlushTestDB` 在建立连接或发出清理前硬拒绝 live DB 0，并校验客户端实际 addr/DB。
- core 的 IAM/share/EventBridge 测试与 worker 的 bus/file/notify 测试中，session、登录锁、Streams、EventBus 等辅助客户端统一改走隔离入口，测试代码不再直接清理共享 Redis。
- TDD 正反路径：`cd libs; go test ./testredis` 首次红为 `no non-test Go files`，首次绿为 `ok excel-libs/testredis`；新增 API 用例后再次红为 `undefined: testredis.Open` / `undefined: testredis.FlushTestDB`，同一命令随后转绿；`go test ./testredis -count=3` 通过。
- 全量/构建：core `go test -p 1 ./... -count=1` 全部 15 包通过（691.835s）；worker 同命令全部包通过（158.226s）；libs/core/worker/web_api 的 `go build ./...` 均通过。
- live stack 运行态验收：Redis DB 0 前后均为 21 个 Streams、同一组 25 个 consumer-group 关联，pending 为 0；calc/notify/tracking 消费组保持活动且部分 last-delivered ID 前进，audit 消费组仍在且有 1 个 consumer。
- 文档守卫：`powershell.exe -NoProfile -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1` 退出码 0（`fail=0`）。
