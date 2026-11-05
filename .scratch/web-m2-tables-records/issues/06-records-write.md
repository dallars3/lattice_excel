# 06: 记录写入（新增·编辑·批量·删除·撤销·冲突）

**What to build:** 用户能在网格里真正维护数据：三种入口新增记录（底栏按钮、末行方向下键、首行幽灵行），四种通道进入编辑（双击、F2、Enter、直接打字），Enter/Tab 提交并移动、Esc 取消不落盘；提交后本地立即生效并在失败时保留输入；发生版本冲突时按三动作裁决；支持选区 TSV 复制与单值/单行粘贴并报告跳过格数；支持勾选行批量删除、复制记录、7 秒内撤销删除、会话内 Ctrl+Z 撤销；无排序视图下可拖行手动排序。

**Blocked by:** 05（记录读取与 11 类单元格渲染）

**Status:** resolved

- [x] 底栏「+ 新增记录」、末行方向下键、首行幽灵行三条入口都能创建记录，创建后焦点落在主字段并进入编辑
- [x] 双击、F2、Enter、直接打字四种通道进入编辑；单击仅选中不进入编辑
- [x] Enter 提交并下移、Tab 提交并右移、Shift+Tab 左移、Esc 取消不落盘；提交期间顶栏呈「保存中」
- [x] 提交成功后本地立即应用并记录版本；失败保留输入、显示行内错误并可重试
- [x] 4001 冲突弹窗展示对方新值与三动作（采用对方 / 仍用我的覆盖 / 复制我的内容），不静默覆盖
- [x] 4002 记录或字段已删除时弹窗说明并保留可复制内容，不自动重建
- [x] 选区复制为 TSV；单值与单行粘贴经批量接口落库，只读列跳过与选项不匹配置空分别计入提示
- [x] 勾选一行或多行可批量删除（二次确认）与复制记录（读+建组合，不复制评论与历史）
- [x] 删除后底栏浮出「已删除 n 条 [撤销]」并保留 7 秒，撤销后数据恢复；超时入口消失
- [x] Ctrl/Cmd+Z 撤销本次会话内最近一步编辑，刷新后不可撤销
- [x] 视图无排序与分组时可拖行手动排序；有排序/分组时拖行被禁止并提示「当前为排序视图」
- [x] 达到单表 5 万行上限时新增被拒并提示联系管理者
- [x] 组件测试经 MSW 覆盖新增/编辑/提交/冲突/删除撤销/批量粘贴/拖行约束；补偿逻辑（撤销与乐观回滚）以纯逻辑单测覆盖

## Answer

**实现**
- 写入 API：H-50 新建、H-51 单元格更新（query base_id、无 client_request_id）、H-52 批量（读 `skipped_count`/`fail_row_list`）、H-53 删除、H-54 撤销、H-49+H-50 复制（排除公式/引用与空值）。
- 乐观更新管线：`applyOptimisticCell`/`rollbackCell`/`removeRecords`/`appendRecord` 纯函数；成功落版本、失败回滚、0 与 null 严格保留；4001 三动作与 4002 内容保留。
- 编辑器：text/number/select/multiselect/date/person/url/relation 八类 + `registerM2Editors`；日期自实现日历与三格式解析（最小 1900）；数字非法字符不可录与超精度四舍五入；选项无匹配提示与 H-40 新建；URL 仅 http/https 自动补 https://。
- 网格：三入口新增、四通道编辑、Enter/Tab/Shift+Tab/Esc、选区 Shift+方向扩展、Delete 经 H-52 置空、Ctrl+C/X/V/Z；TSV 复制与单值/单行粘贴（G-5 二维提示）；批量删除/复制；7 秒撤销窗口（uiStore，跨页面按 deadline 恢复）；会话内 Ctrl+Z 模块级栈；拖行本地排序与排序视图禁止。
- 接线与权限：底栏新增/批量条/撤销条；可查看隐藏新增与批量入口；可填写仅编辑自己创建的记录；5 万上限禁用新增并提示。

**文件**
- 新增：`web/src/features/records/{write-api,optimistic,clipboard,edit-history,capacity}.ts` 及测试；`web/src/core/grid/renderers/{editors,editor-values}.tsx|ts` 及测试与样式；`web/src/features/workspace/components/record/RecordConflictDialog.tsx`。
- 修改：`web/src/features/records/api.ts`（导出 RawRecord/toNumber）；`web/src/core/grid/{registry,index}.ts` 与 `renderers/{index,text}.tsx`；`web/src/features/workspace/{BaseWorkspacePage.tsx,components/{GridArea.tsx,GridArea.module.css,WorkspaceFooter.tsx}}` 及对应测试；`web/src/stores/ui.ts`、`stores/index.ts`、`auth/logout.ts`、`stores.test.tsx`、`BaseWorkspacePage.module.css`。
- 本票面。

**测试**
- `npm run test`：67 个文件 / 869 用例全绿（基线 765，新增 104）。
- `npm run build`：`tsc --noEmit` + Vite 构建成功，首屏 6 个 JS chunk。

**偏差与 G 项**
- G-06-1 手动排序仅本地调整行序、不持久化：契约无记录排序字段；`hasSortOrGroup` 已由父组件注入，票 07 接线视图配置前页面传 `false`。
- G-06-2 粘贴/Delete 清空走 H-52 时 `expect_version=0`（契约语义「0=不校验版本」），避免同一记录多字段批量自造版本冲突；服务端 `skipped_count`/`fail_row_list` 已并入提示。
- G-06-3 附件字段在网格内只读（上传/管理归票 08 抽屉）。
- 4002 仅弹窗说明 + 复制内容 + 失效记录/字段查询，不自动重建（按需求）。
- G-06-4：H-52 批量写入成功后按 `result_list` 写回记录版本、失败项按 before 回滚，避免后续 H-51 使用陈旧版本造成假 4001 冲突。
- G-06-5：新增记录成功后追加到当前无限查询缓存末页（不立即 invalidate 记录 key），保证新行可见并让主字段进入编辑。
