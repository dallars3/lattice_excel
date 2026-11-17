# 04: 记录行级越权错误码

**What to build:** 「可填写」协作者尝试修改、删除、撤销、回滚他人记录时收到明确的 3001 无权限，数据保持不变；记录不存在或已删除仍返回 4002；前端据此转只读提示。

**Blocked by:** None (can start immediately)

Status: done

- [x] 可填写用户更新他人记录单元格返回 3001，原值与原版本不变
- [x] 可填写用户批量更新、删除、撤销删除、回滚、读历史命中他人记录时返回 3001
- [x] 记录不存在或已删除仍返回 4002，不与权限拒绝混淆
- [x] 可查看用户的写操作仍返回 3001
- [x] 前端全局错误处理将 3001 呈现「权限已变更，页面已转为只读」
- [x] TC-080-04 通过；TC-080-01/02 的数据范围与只读行为不回归
- [x] 补 ADR 记录「可填写用户可感知记录是否存在」的信息披露权衡

## Comments

根因：记录域在 `ROW_SCOPE_OWN` 下把「记录存在但创建者非当前用户」与「记录不存在/已软删除」统一映射为 4002（`records/update_cell.go`、`batch_update.go`、`delete_records.go`、`undo_delete.go`、`rollback.go`、`get_history.go`）。前端因此按 4002 走「刷新」恢复策略，既不提示无权限，也不触发只读降级，与 AC-PERM-03 和网关 CheckAccess 的 3001 口径不一致。

改动：
- 六个写/历史入口的越权分支改为 `deal.CodePermissionDenied`（3001）；批量更新写入逐 op `error_code`。
- 保持判定顺序「先存在/可操作、后归属」：记录不存在或已软删除仍为 4002。
- `row_scope_write_test.go` 改为断言 3001 并补齐批量更新、删除、撤销、回滚、读历史、缺失/已删除 4002 用例；`get_history_test.go` 越权断言改为 3001。
- 前端 `GlobalErrorBridge.test.tsx` 补 3001 用例，确认映射「你的权限已变更，页面已转为只读」并置 `forcedReadOnly`；映射实现未改。
- 新增 `docs/adr/0003-row-scope-own-permission-code.md`；接口文档 v0.7 §8.0.1 与 H-51/H-52/H-54/H-55/H-56 同步 3001 备注。

验证：
- `cd excel_core_service && go test -p 1 ./... -count=1` → 全绿（本地运行中的 core/worker 服务会抢占 outbox 事件，运行时临时挂起后通过）。
- `web`：Vitest `GlobalErrorBridge.test.tsx`、`error-catalog.test.ts` 全部通过；`tsc --noEmit` 与 `vite build` 通过。
- `powershell -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1` → 退出码 0。
