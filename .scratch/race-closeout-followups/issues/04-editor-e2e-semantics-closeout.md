# 04: 下拉编辑器 E2E 语义收口

**What to build:** 修正多选、人员、关联字段的 E2E helper，使其执行完整用户操作：选择或勾选候选后清空搜索词，再按 Enter 提交完整集合。TC-040-07/09/14 解封并通过，不再以错误的产品缺陷 fixme 跳过正确实现。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 多选、人员、关联 helper 完成真实交互链：输入与点击候选、确认已选标签、清空搜索词、按 Enter 提交、等待编辑器收起
- [x] 提交后目标单元格展示全部期望标签，并通过 API 或刷新读回确认值已落库
- [x] TC-040-07、TC-040-09、TC-040-14 从 fixme 解封，注释改为“验收 helper 未完成清空搜索后的提交动作”，不再描述为产品提交链缺陷
- [x] 组件测试明确覆盖：多选点击保持打开、搜索非空 Enter 只勾选不提交、搜索清空 Enter 提交并收起、单选点击候选立即提交并收起
- [x] 不改变既有键盘状态机、单选/多选交互语义或弹层信息架构
- [x] 编辑器既有组件测试与 p1p2 全量套件保持绿，三条新解封用例正常通过

## Comments

### 2026-11-20 实现与验证交接

**实现：** `editMulti`、`editPerson`、`editRelation` 均改为完整用户链：点击候选、确认已选标签出现、清空搜索词、按 Enter 提交并等待编辑器隐藏。`stringListCellValue` 解码 protobuf 列表单元格供落库读回；TC-040-07/09/14 已解封，并分别以 option_id、编辑器用户 ID、目标 Record ID 做 API 读回断言。integration commit `4f9c2d1` 已合入；E2E 实现提交为 `8bd8cc9`。集成提交中的代理文件排除正则有一处转义错误，已以 `ef7e1f0` 修正，未修改 `.codex-agent-brief.md` / `.codex-agent-report.md`。

**红/绿证据：** helper 修复前，TC-040-07/09/14 均真实红在 editor `toBeHidden` 断言：非空搜索词按 Enter 只执行候选匹配/勾选，没有触发提交；修复后同一断言转绿。产品侧无改动，现有 41 条编辑器组件测试已明确覆盖四条契约：“multiselect 点击为勾选切换且编辑器保持打开（不提交）”“multiselect 搜索词+回车：先匹配勾选且编辑器保持打开，不提交旧集合”“multiselect 方向键高亮 + 回车勾选高亮项（保持打开）；再次回车提交已选集合”“person 单选：点击候选即提交并收起”。

**本轮验证：**

- live stack 目标用例单命令：TC-040-07、TC-040-09、TC-040-14 **3 passed / 0 failed**（9.9s）。
- `npm test -- src/core/grid/renderers/editors.test.tsx`：**41/41 passed**。
- `npm run build`：TypeScript 检查及 Vite production build 通过。
- `powershell.exe -NoProfile -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1`：**exit 0，fail=0**。

**全量证据与遗留交接：** 既有 p1p2 全量运行结果为 **50 passed / 4 failed**；TC-040-07/09/14 在该全量运行中均通过。四条失败均与本票编辑器语义无关，按既有隔离结论交接：TC-180-03 网格停滞超时交 **06** 做 trace/稳定性收口并交 **07** 最终复跑；TC-010-10 注册 5003 限流环境产物、TC-040-08 与 TC-050-16 的隔离即通过瞬态，均交 **07** 的最终 live-stack 全量门复核。最后一项全量复选框保持未勾选，本票保持 `ready-for-agent`/open；仅当 06/07 达成 p1p2 `54/54` 后再正式关单。

### 2026-11-21 最终全量收口

- 隔离 live stack 最终 p1p2：**54 passed / 0 failed**；TC-040-07、TC-040-09、TC-040-14 均正常执行且通过，无 skip/fixme。
- TC-040-07 的最后“移除红色选项”现在同时等待编辑器收起和 API 读回仅剩 `blueId`，消除了后续 TC-040-08 可能读到旧版本的跨用例写读竞态。
- 编辑器组件契约测试：**41/41 passed**；BaseWorkspacePage 视图配置与记录查询竞态组件测试：**29/29 passed**。
- `npm run build`（`tsc --noEmit && vite build`）通过。

证据：`F:\codex\wt\rf-07\web\e2e\p1p2.spec.ts` 的最终 p1p2 执行记录，以及 07 live stack Playwright 运行输出。

