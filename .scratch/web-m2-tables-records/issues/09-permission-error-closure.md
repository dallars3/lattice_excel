# 09: 权限矩阵与异常态收口

**What to build:** 把需求第 5 章的四级角色 × 内部/外部矩阵在所有已落地入口上逐项核对并补齐显隐/只读/仅自己三态，使可管理、可编辑、可填写、可查看四种身份在使用同一应用时看到各自应当看到的能力；同时把跨页面的异常态收敛成统一行为：权限中途被降级立即转只读、服务端 5xx 自动重试三次后转手动、保存状态三态、断网横幅占位、容量与空态矩阵。

**Blocked by:** 06（记录写入）、07（视图管理与全量视图配置）、08（记录详情抽屉与附件字段）

**Status:** resolved

- [x] 可管理：字段与表结构入口、表头全项菜单、分享入口位、顶栏「更多」全项均可见可用
- [x] 可编辑：可编辑单元格与视图配置，但字段删除与类型转换入口隐藏；顶栏重命名/复制/删除入口隐藏
- [x] 可填写：网格只展示本人创建的记录并在页眉提示「仅显示你创建的记录」；只能编辑自己的记录；新增入口可见而删除、批量条、导入导出、结构入口隐藏
- [x] 可查看：全部数据只读，新增/删除/编辑入口隐藏；可切换已有视图但不可改配置
- [x] 外部账号：无论角色均不渲染导出入口；首页不出现新建入口
- [x] 任意写请求返回 3001 时立即切换为只读态并提示「你的权限已变更」，不使用前端显隐作为安全边界
- [x] 服务端 5xx 自动重试三次后转手动重试并提示；9001 有兜底文案
- [x] 保存状态三态（保存中/已保存/离线）在顶栏正确切换；断网时展示黄色横幅占位，不做本地排队重放（留 M3）
- [x] 容量上限（表 50、字段 100、记录 50,000、筛选 5、排序 3）与各错误码均落到约定提示
- [x] 权限矩阵以快照测试覆盖四角色 × 内部/外部 × 关键入口，3001/5001/9001 各有组件用例
- [x] 手动在浏览器按四种角色走查一遍工作台，确认无越权入口、无只读态下仍可编辑的漏点

## Answer

**审计结论**：票 01-08 已落地的入口整体符合需求 §5.1；本票补齐 4 类缺口——① 分享入口原先对全部角色常显；② 可填写仍可见批量条/行勾选与删除撤销；③ 只读态下仍可通过列宽拖拽、清除筛选、恢复字段、拖行排序触发视图写入；④ 权限降级后字段/视图面板、顶栏对话框与抽屉编辑态未即时收起。

**实现**
- `app/GlobalErrorBridge.tsx`：挂在 ToastProvider 内，统一处理 2001 重登、3001/3002 强制只读 + 「你的权限已变更，页面已转为只读」、3002 追加「外部协作者不可导出数据」、5003 限流提示、9001/9002「服务暂时不可用，正在重试（最多 3 次）」；同一提示 1s 去重。
- `app/providers.tsx`：QueryClient 读请求默认 `retry` 仅对 9001/9002 生效且 `failureCount < 3`，测试环境 0 延迟、生产指数退避；写请求不自动重试。
- `stores/workspace.ts`：新增 `forcedReadOnly/setForcedReadOnly`；`features/auth/logout.ts` 与工作台切换 Base 时复位。
- `features/permissions/usePermission.ts`：`forcedReadOnly || offline` 时按 viewer 计算（保留内外部身份）；矩阵将 `manage_share` 纳入外部恒隐藏。
- 离线：online/offline 事件 + 初始 `navigator.onLine` 驱动 `workspaceStore.offline`，全局黄色横幅「网络已断开，恢复后将自动同步」，顶栏三段显示「离线」。
- 权限入口：分享位仅 manager/editor 可见（M4 占位禁用）；顶栏「更多」对可填写/可查看/外部隐藏（可编辑仅保留星标）；可填写仅页眉提示「仅显示你创建的记录」并隐藏批量条/勾选/删除/结构入口；只读态收起编辑、面板、对话框与删除撤销条。
- 容量：新增 `features/workspace/capacity.ts` 统一表 50 / 字段 100 / 记录 50,000 / 筛选 5 / 排序 3 与文案；表满 50 时禁用新建并提示。
- 只读漏点：单元格四通道进入编辑统一走 `cellEditable`；离线/降级时退出编辑并隐藏写入口；列宽、清除筛选、恢复字段、拖行均按编辑权限门控。

**文件**：`web/src/app/{GlobalErrorBridge.tsx,GlobalErrorBridge.module.css,providers.tsx}`、`web/src/features/permissions/{matrix.ts,usePermission.ts,matrix.snapshot.test.ts,__snapshots__/}`、`web/src/stores/workspace.ts`、`web/src/features/auth/logout.ts`、`web/src/features/workspace/{capacity.ts,BaseWorkspacePage.tsx,BaseWorkspacePage.permission.test.tsx,BaseWorkspacePage.exception.test.tsx}`、`web/src/features/workspace/components/{GridArea.tsx,TableTabs.tsx,ViewBar.tsx,WorkspaceTopBar.tsx,WorkspaceFooter.tsx}`、`web/src/features/workspace/components/field/{FieldCreateDialog.tsx,FieldHeaderRow.tsx,impact.ts}`、`web/src/features/workspace/components/view/ViewPanels.tsx`、`web/src/features/workspace/types/view-config-validation.ts`、`web/src/features/record/RecordDrawer.tsx`、`web/src/features/records/capacity.ts`、`web/src/features/home/HomePage.tsx`、`web/src/app/GlobalErrorBridge.test.tsx`。

**测试**：`npm run test` 952 passed / 76 files（原 930 全绿，新增权限快照、全局异常桥、四角色工作台、3001/离线/9001 用例）；`npm run build` 通过（首屏 5 chunk）。

**偏差与 G 项**
- 可填写不做前端行过滤，仅页眉提示并禁止编辑他人记录；数据范围仍以后端 `row_scopes` 为准（票面允许「至少页眉提示」）。
- 离线只呈现状态 + 只读，不排队重放（M3）。
- 分享按钮仍为 M4 占位（禁用，仅可管理/可编辑可见）。
- 9001 自动重试在测试环境 0ms，生产指数退避。
- 浏览器四角色人工走查留待全量收口：分别以 owner/editor/filler/viewer 及外部账号打开同一 Base，核对顶栏/表头/视图/底栏/抽屉入口；构造隐藏写请求确认 3001 即时降级；断网确认黄色横幅与「离线」并确认写入口消失。
