# 02: 新建数据表默认结构

**What to build:** 新建数据表后立即拥有默认主字段与默认表格视图，服务端一次事务内保证，任何 API 调用方都拿到一致的可用表；前端现有兜底变为幂等防御。

**Blocked by:** None (can start immediately)

Status: done

- [x] 新建数据表成功后立即查询字段与视图，各有且仅有 1 条：主字段「标题」（多行文本）、视图「表格」（表格视图）
- [x] 数据表摘要中的字段数与视图数均为 1
- [x] 数据表行、默认字段、默认视图在同一事务内创建，任一步失败不留下半张表
- [x] 前端默认结构兜底保持幂等：检测到已有字段/视图时不重复创建
- [x] TC-030-01 UI 通过；API 侧新表默认结构断言语义一致
- [x] 既有新建表、重命名、复制、删除表用例不回归

## Comments

**根因：** `internal/structure/create_table.go` 只调用 `repository.CreateTable` 插入数据表行，默认字段与视图依赖前端 `bootstrapTableDefaults` 补建；非前端调用方拿到的是无法录入记录的空表，摘要计数恒为 0/0。

**改动：**

- `excel_core_service/base/structure.go`：新增 `CreateTableWithDefaults(ctx, table, primaryField, defaultView)`，在同一事务内依次插入数据表行、默认主字段、默认视图，任一步失败整体回滚；抽出 `insertStructureView` 供新建与复制复用；`CreateTable` 保留给不建默认结构的内部路径。
- `excel_core_service/internal/structure/create_table.go`：建表时构造默认结构并调用仓库事务方法。口径与前端兜底一致：主字段「标题」`FIELD_TYPE_TEXT`、`config={}`、`is_primary=true`、`position=0`；默认视图「表格」`type=table`、`config={}`、`position=0`；返回摘要 `field_count/view_count=1`。
- `excel_core_service/internal/structure/create_table_defaults_test.go`（新增）：包级真实 MySQL 测试锁定默认字段/视图属性、摘要 1/1、列表查询 1/1，以及默认视图插入失败时无半张表残留（事务回滚）。
- 既有结构测试按新语义调整断言：`field_config_test.go` 字段基数 +1；`rpc_lifecycle_test.go` 复制表断言改为默认「标题」+ 重命名字段、视图 4 条；`view_capacity_test.go`/`view_field_width_test.go` 新建视图改名以避开默认「表格」。
- `web/src/features/workspace/BaseWorkspacePage.tsx`：仅更新注释，说明服务端已下发默认结构，前端兜底只处理历史空表且幂等（实现本身已是「空列表才创建」）。
- `web/src/features/workspace/BaseWorkspacePage.test.tsx`：新增 vitest，服务端返回 1 字段 1 视图时不再 POST 字段/视图。

**验证：**

- `cd excel_core_service; go test -p 1 ./...` → 全绿（含 structure 42.9s、base 130.7s）。
- `cd web; npm run build` → 通过；`npm test` → 78 个文件 / 1007 条全绿（含新增幂等用例）。
- `powershell -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1` → 退出码 0。
