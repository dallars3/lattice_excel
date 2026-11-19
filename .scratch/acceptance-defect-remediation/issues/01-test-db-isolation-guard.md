# 01: 测试库隔离护栏（prefactor，数据事故护栏）

**What to build:** 核心与 worker 服务的后端测试一律使用独立测试库：测试框架按固定后缀（如 `excel_core_test`/`excel_worker_test`）自建库、跑迁移、测试结束后清理；现行「清空当前库全部表」的复位路径改为仅作用于测试库，并在执行任何清库/迁移操作前硬校验目标库名带测试后缀，非测试库直接失败并给出清晰报错。护栏落地后，在 live stack 运行状态下跑全量核心测试不再影响共享 dev 库。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 核心服务测试基建改为独立测试库模式（实现口径：缺表时经迁移建表；**缺库不自动建库**——testmysql.Open 报错附一次性 CREATE DATABASE/GRANT SQL 提示，由人工执行，护栏自身绝不成为共享库写入源）→ 迁移 → 用例 → 清理
- [x] worker 服务测试基建同口径
- [x] 任何清库/迁移 helper 在目标库名不带测试后缀时硬失败（含集成测试复位路径），报错指明原因
- [x] live stack 运行状态下跑核心全量 go test：共享 dev 库（excel_core/excel_worker）的表行数与内容前后不变（用计数断言验证）
- [x] 既有测试全部保持绿（切换测试库后语义不变）
- [x] 用例与护栏行为记录进测试基建文档注释（防下一个人再踩）

## Comments

**实现记录（2026-11-19）**
- 分支 `fix/ad-01-test-db-isolation-guard`，提交 `6aa53ec`（44 文件 +746/−179），并入集成分支 `fix/web-m2-grid-fixes`（合并提交 `9399f30`）
- 要点：新增共享护栏包 testmysql（TestDSN/TestSchemaName 派生测试库、ValidateTestDatabase `_test` 后缀硬校验、Open/OpenTestDB 连接唯一入口非测试库名建连接前即失败、缺库报错附一次性建库 SQL 提示不自动建库）；core 33 处测试连接 + 7 处硬编码 DSN + 3 处 dropAllTables + 迁移重放全部接入；worker 同口径并把跨库引用改经 CoreSchemaName（测试期覆盖 excel_core_test）
- 验收硬证据：internal/testisolation/snapshot_test.go + scripts/verify-test-db-isolation.ps1（record → 全量 go test → verify 逐表计数不变；经篡改注入验证非同义反复）；live stack 全程运行下 core 14 包 + worker 9 包全绿，共享库快照不变；audit/calc 两个历史环境性失败三次全量均未出现
- 备注：测试仍会 FlushDB 共享 Redis（既有行为，票面仅限 MySQL 隔离）——Redis 隔离建议后续票评估
