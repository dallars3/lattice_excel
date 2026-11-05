# 02: 网格编辑提交落库与保存状态诚实化

**What to build:** 用户在网格编辑单元格并提交（Enter/Tab/失焦）后，数据真实落库（刷新页面与服务端读回一致），顶栏保存状态如实反映「保存中/已保存/保存失败」，失败时回滚乐观值并展示可解释文案（4001 冲突走既有三动作弹窗数据）。修复后「编辑 → 刷新 → 值还在」成立。

**Blocked by:** 01: H-48 出参契约与空表网格修复（同根因链：先保证 H-48 条目形状正确，再诊断编辑提交断点，验收才可信）

**Status:** done

- [x] 诊断并修复编辑提交断点：编辑提交必发出 UpdateCell 请求（冒烟证据：合成事件与 CUA 真实键鼠双路径均零 PUT；`submitCell`/`trackWrite`/编辑器 onCommit 接线以实际断点为准，不臆测）
- [x] 顶栏保存状态三态如实：保存中（写请求在途）/已保存（成功）/保存失败（失败并回滚乐观值）；禁止在未发出或失败时显示「已保存」
- [x] 提交失败路径回归：1001 行内错误内容不丢；4001 进入冲突态（带 current_value/current_version）；4002/9001 各自文案
- [x] 组件测试：GridArea 编辑提交用例（基于 01 的真实形状 fixture）断言 fetch 发出、成功更新、失败回滚；`npm run test` 全绿
- [x] 部署实例浏览器走查：编辑单元格 → 刷新 → 值还在；`npm run build` 成功、check-docs 全绿

## Comments

- 2026-11-06 诊断结论（票面「以实际断点为准」）：`submitCell`→`trackWrite`→`updateCell`→PUT 链路在源码中完好（jsdom 全路径可复现 PUT）。真实断点有三处：①编辑器无失焦提交通道，且 `handleCellClick` 点击他处直接 `setEditing(null)` **静默丢弃草稿**——真实浏览器中 IME 合成吃掉单次 Enter 后的点击离开正落在此处，顶栏却因 `inFlightWriteCount===0` 显示「已保存」；②`handleGridKeyDown` 在编辑态早退，视口级 Enter/Tab/Esc 提交/取消语义（AC-REC-02 编辑态分支）为不可达死代码；③`deriveSaveState` 把「无在途写」直接映射「已保存」，无 failed 态。修复：editorHost `onBlur`（relatedTarget 守卫，选项面板/日历/重试按钮间焦点移动不打断）+ 点击离开提交 + 编辑态三键兜底 + 单飞 ref（blur 与 click/Enter 重入只发一次 PUT）+ 未改动草稿零 PUT 收起；`SaveState` 增 `'failed'`，`deriveSaveState(offline, saving, failed, saved)` 排序 offline > saving > failed > saved。提交 `1aa64c5`、`525fab4`。
- 2026-11-06 4001 键对齐：接口文档 v0.7 §8.6.3 承诺 `data:{their_value, their_version}`，前端 `parseConflictMeta` 本就按此解析；后端 `update_cell.go` 由 `current_value/current_version` 改为文档键，契约测试+组件 fixtures 全链单键（票面 4001 括号内键名为后端旧键，以文档为准）。提交 `58fb71e`、`c88e1fe`。
- 2026-11-06 部署实例走查：编辑标题单元格 → 提交 → 服务端读回 `cells_map` 含新值且 `version:2` → 刷新页面 → 值仍在（网格渲染「第一条走查记录」）；顶栏三态切换正常。code-review 后追加：视口键盘兜底统一走 `commitPendingDraft` 未改动跳过（`5d04ce1`），未改动 Enter/Tab 零 PUT 仍按移动语义收起。

**冒烟证据**：编辑「第一行标题」+Enter → 零 PUT（performance entries 空）→ 服务端 `cells_map` 恒空（两条记录均 undefined）；部署 bundle 与源码构建 hash 一致（index.SBPH_vS4.js）→ 源码缺陷。
