# 03: Base 工作台骨架与数据表管理

**What to build:** 从首页进入某个 Base 后，看到与需求一致的五段式工作台：顶栏（Base 名称、保存状态位、在线头像位、分享入口位、更多菜单）、表与视图栏、网格区、底栏。用户能在表标签之间切换，并新建、重命名、复制、删除数据表；最后一张表禁止删除。有在途写请求时表标签显示「保存中」微状态。本票只交付骨架与表结构管理，网格数据与视图切换由后续票接线。

**Blocked by:** 02（首页 Base 聚合与 Base 管理）

**Status:** resolved

- [x] 通过深链进入 Base 时按地址加载 Base 信息、表列表与默认视图，页面呈五段式布局
- [x] 表标签页展示全部数据表并切换当前表；切换后视图栏与网格区跟随当前表
- [x] 新建表（1–30 字符、同 Base 重名返回冲突提示）后自动切换为当前表
- [x] 重命名、复制、删除表可用；仅剩一张表时删除入口禁用；删除需二次确认
- [x] 顶栏「更多」按角色提供重命名/复制/删除/星标，删除为强提示二次确认
- [x] 存在在途写请求时当前表标签显示「保存中」，结束后恢复
- [x] 底栏展示「+ 新增记录」入口位置与记录数占位，网格区在无数据票时呈骨架空态
- [x] 组件测试经 MSW 覆盖表列表加载、表 CRUD、最后一张表禁删与深链进入

## Answer

**实现**

- Base 兜底：`features/workspace/api/base.ts` 的 `fetchBaseById(baseId)` 经 H-21 聚合四列表（最近访问/我创建的/与我共享/星标）按 id 查找，命中即返回，四列表均无则 `null`；页面呈「你没有访问该表格的权限」+「返回首页」。Base 重命名/复制/删除/星标直接复用 `features/home/api.ts` 的 H-24/25/26/27。
- 表结构 API：`api/table.ts` 覆盖 H-29 列表、H-30 新建、H-31 重命名、H-32 复制、H-33 删除，全部 `attachClientRequestId:false`；H-32 无请求体、H-33 body `{confirm:true}`；Table 归一化 `created_at` 为 number。`api/view.ts` 仅实现 H-41 列表并归一 View。
- query key 与失效：新增 `tableKeys.list(baseId)`、`viewKeys.list(tableId)` 并导出；表结构写成功后失效当前 Base 表列表，删表时 `removeQueries(viewKeys.list(tableId))` 清理对应视图查询。
- 工作台状态：新增 `stores/workspace.ts`（`workspaceStore/deriveSaveState/trackWrite/useSaveState`），在途写计数驱动顶栏「保存中…/已保存」与当前表标签「保存中」，`offline` 布尔作为票 09 占位（offline 优先于 saving）；所有页面写动作经 `trackWrite` 包裹。
- 五段式骨架：`BaseWorkspacePage.tsx` 深链并行发起 H-21 与 H-29，默认当前表=第一张表，默认视图=该表第一个视图；Base/表/视图分别有错误态与重试。组件拆为 `WorkspaceTopBar`（名称、保存状态、在线头像占位、禁用分享、更多菜单）、`TableTabs`（切换/新建/重命名/复制/删除、最后一张表禁删、保存中微状态）、`ViewBar`（视图列表与本地切换、工具栏按钮位预留）、`GridArea`（骨架空态）、`WorkspaceFooter`（禁用「+ 新增记录」与「已加载 0 条」）。
- 权限：表结构入口（新建/重命名/复制/删除表）仅 `manage_structure==='allow'` 渲染；顶栏「更多」按 `manage_base`/`star` 显隐，外部账号不出现「更多」；删除 Base/表均为「删除后不可恢复（P0）」二次确认。Base 重命名 1–50，新建/重命名表 1–30，重名 1002 在对话框内行内提示并保留输入。

**文件**

- 新增：`web/src/features/workspace/api/{base.ts,table.ts,view.ts}` 及对应 `*.test.ts`、`web/src/features/workspace/errorText.ts`、`web/src/features/workspace/components/{WorkspaceTopBar,TableTabs,ViewBar,GridArea,WorkspaceFooter}.tsx`、`web/src/features/workspace/BaseWorkspacePage.test.tsx`、`web/src/query-keys/{table.ts,view.ts}`、`web/src/stores/workspace.ts` 与 `web/src/stores/workspace.test.ts`。
- 修改：`web/src/features/workspace/BaseWorkspacePage.tsx`、`BaseWorkspacePage.module.css`、`web/src/query-keys/index.ts`、`web/src/stores/index.ts`；同步更新既有深链用例：`web/src/app/router.test.tsx`、`web/src/features/home/HomePage.test.tsx`、`web/src/features/auth/LoginPage.test.tsx`（工作台标题改为 Base 名称并补 H-29/H-41 mock）。

**测试**

- `npm run test`：50 文件 / 604 用例全绿（票 02 基线 572，新增 32）。
- `npm run build`：TypeScript + Vite 构建成功，首屏 5 个 chunk（≤6）。

**偏差与契约缺口**

- 契约无 `GET /bases/{id}`（登记 G）：工作台用 H-21 聚合兜底；Base 不在四列表时按无权限态处理，无法区分「不存在」与「无权限」。
- H-41 依赖「当前表」：Base（H-21）与表列表（H-29）并发发起，视图列表在首张表确定后发起，非三者同时并发（无跨表视图聚合接口）。
- `offline` 仅状态占位，未接离线队列/重放（票 09）。
- 本票未接字段与记录（票 04~06）、视图 CRUD 与配置（票 07）、记录抽屉（票 08）；网格与底栏数据为骨架占位。
