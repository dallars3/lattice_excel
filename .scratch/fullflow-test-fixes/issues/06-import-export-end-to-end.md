# 06: 导入导出全链路

**What to build:** 可管理者导入 CSV/XLSX 后数据正确落入新 Base，脏行只跳过坏行并给出行级报告；导入失败不留可访问的半成品 Base；导出按当前视图的筛选与可见字段生成命名规范的文件。

**Blocked by:** 03（失败导入的清理复用 Base 删除能力）

Status: done

- [x] worker 调用 core 的记录读写携带行级范围（全部）与企业、Base、数据表元数据，不再报元数据缺失
- [x] CSV 导入 3 行全部成功入库，新 Base 带可用的默认结构
- [x] XLSX 导入 3 行全部成功入库，行为与 CSV 一致
- [x] 含脏数据的导入只跳过坏行，成功行入库，行级报告含行号/列名/原值/原因
- [x] 导入全行失败或任务失败时，不留可访问的半成品 Base；成功完成后才解除隔离
- [x] 导出 CSV 与 XLSX 均只含当前视图筛选结果与可见字段
- [x] 导出文件名为 `Base_表_视图_YYYYMMDD.{xlsx|csv}`，公式与引用输出计算值
- [x] 外部协作者直调导出返回 3002；可查看/可填写用户导出按矩阵拒绝
- [x] TC-140-01/02/09 通过；TC-140-10 权限语义不回归
- [x] 包级测试覆盖 worker 转发元数据、导入失败清理、导出视图范围与命名

## Comments

**根因：**

1. worker 的 `forwardingContext` 只转发 `x-principal/x-enterprise-id/x-base-id/x-request-id`，core records 的 `rowScopeFromContext` 强制要求 `x-row-scope`（单值），导致导入逐行写入与导出 QueryView 全部报 `x-row-scope metadata is required`。
2. 修好元数据后仍有数据正确性缺陷：导入按单元格发 `BatchUpdate`，而 core 对 `record_id` 为空的每个 op 各建一条记录，2 列表格 3 行会落成 6 条单列记录。导入改为按行调用 `CreateRecord`（一次写入整行 cells_map）。
3. 全行失败仍以 `DONE + ok_rows=0` 收尾，新建的「导入中」Base 在 `import.finished` 被消费后解锁，留下可访问空壳。

**改动：**

- `excel_worker_service/internal/importexport/common.go`：新增 `coreCallContext(baseID, tableID)`，转发 `x-principal/x-enterprise-id/x-base-id/x-table-id/x-action/x-request-id` 并强制写入单值 `x-row-scope=ROW_SCOPE_ALL`；已知 Base/表时覆盖为任务目标值。
- `excel_worker_service/internal/importexport/create_import.go`：所有 core 调用改用 `coreCallContext`；`writePreparedRows` 改为按 CSV/XLSX 行调用 `CreateRecord`；新增 `failAllRowsImport`——全行失败时写行级报告、任务置 FAILED、删除本次新建的 Base 并从任务中清空 base/table，不发布 `import.finished`（不会解锁一个已删 Base）；清理调用同样携带裁决元数据。
- `excel_worker_service/internal/importexport/create_export.go`：导出取数/命名/字段读取统一用 `coreCallContext`（视图筛选、排序、隐藏字段仍由 core `QueryView` + `GetViewConfig` 保证，公式/引用由 `QueryView` 物化后输出）。
- `excel_worker_service/rpc/records.go`：`RecordsRPC` 增加 `CreateRecord` 客户端方法（复用既有 proto，未新增 RPC）。
- `excel_web_api/internal/gateway/importexport_routes.go`：`importWorkerContext` 增加动作参数，向 worker 显式传递 `x-action=ACTION_IMPORT/ACTION_EXPORT`（内部身份与 Base 仍由 `x-principal/x-enterprise-id/x-base-id` 携带）；状态/报告/下载不带动作。外部协作者导出在路由层仍返回 3002，可查看用户返回 3001。
- 测试：worker 新增 CSV/XLSX 各 3 行逐行入库、core 调用元数据（行级范围 + 企业/Base/表/principal）、全行失败清理；导出新增转发元数据断言；既有 XLS/GBK/部分失败用例改为断言 `CreateRecord` 的整行 cells。网关新增内部 viewer 导出 3001，原有外部协作者 3002 保留，并断言导入导出转发 `x-action` 与 `x-principal`。

**验证：**

- `cd excel_worker_service; go test -p 1 ./... -count=1` → 全绿（importexport 12.0s）。
- `cd excel_web_api; go test -p 1 ./... -count=1` → 全绿。
- 未改 core / proto；`gen/` 未动。
- TC-140-01/02/09/10 未在本 ticket 内执行部署态 P0 套件（按约定 dev 服务保持停止）；对应契约由上述包级测试锁定，待集成后跑 `p0-api.mjs` 回归确认。

**遗留风险：**

- 新建 Base 导入时 core `CreateTable` 会先建默认主字段「标题」，worker 再追加导入列，导入表会多一列空「标题」；若源表本身含「标题」列则可能字段重名被拒（未在本次处理）。
- 全行失败但导入目标是既有 Base 时只置任务 FAILED，不删除用户已有 Base（符合预期，但前端需按 FAILED 展示）。
- 导入/导出仍走同步执行；大文件沿用现有 5 万行/20MB 上限。


## Comments（主代理收口，2026-11-06）

- 部署态回归补齐三处：
  1. 导入新建 Base 改为 import_pending 时不建默认表（默认表由导入流程按源文件创建），避免新 Base 多一张空表导致 P0 读错表；
  2. core 消费循环修复两处 liveness：Redis Streams 空闲 DeadlineExceeded 不再终止循环；worker 直接发布的 import.finished 改走 RegisterExternal（无 core outbox 行），否则导入完成永远不解锁导入锁；
  3. 中文文件名按 percent-encoding 写入 gRPC metadata（metadata 仅允许 ASCII），网关解码后回填 X-File-Name/Content-Disposition，导出命名不再丢头。
- P0 API/WS 29/29（含 TC-140-01/02/09）、UI 31/31 全绿；contract-test 与 check-docs 均 exit 0。
