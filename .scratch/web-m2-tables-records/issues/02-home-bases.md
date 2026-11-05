# 02: 首页 Base 聚合与 Base 管理（含权限门基座）

**What to build:** 登录用户在首页能看到自己全部可访问的 Base 并按「最近访问 / 我创建的 / 与我共享 / 星标」分区浏览；能按名称搜索、新建 Base 并直接进入工作台；能在卡片上重命名、复制、删除（强提示不可恢复）、切换星标。外部账号首页不出现任何新建入口。本票同时落地统一的权限门基座（由 Base 的 my_role 与内部/外部身份归一成 close/hide/readonly/filter-self 四类 UI 态），供后续票复用。

**Blocked by:** 01（认证闭环）

**Status:** resolved

- [x] 首页按聚合接口渲染四个分区，星标区置顶；卡片展示名称、表数、最近修改时间与修改人、星标状态
- [x] 名称搜索输入 1–50 字符即可过滤并展示结果；无结果与「与我共享为空」有克制空态
- [x] 新建多维表格成功后直接进入该 Base 工作台；未命名时使用默认名称
- [x] 卡片菜单可重命名（1–50 字符）、复制（不复制评论与历史）、删除（「P0 不可恢复」二次确认）、切换星标
- [x] 外部账号首页隐藏新建入口与任何 Base 级管理入口
- [x] 权限门基座可据 Base 的 my_role 与 internal/external 输出四类 UI 态，并有一个入口经它渲染显隐
- [x] 组件测试经 MSW 覆盖聚合渲染、搜索、新建、重命名/复制/删除/星标与外部账号首页

## Answer

**实现**

- 目录迁移：首页落 `web/src/features/home/`（`HomePage.tsx`、`api.ts`、`format.ts`、`components/BaseCard.tsx`、两份 CSS module）；删除 `features/workspace/HomePage.*`，路由改指 `@/features/home/HomePage`，`/home` 标题「协作多维表格」与 HealthNotice 保持。
- API：H-21 聚合、H-22 搜索、H-23 新建（空名默认「未命名多维表格」）、H-24 重命名、H-25 复制（无请求体）、H-26 删除（`confirm:true`）、H-27 星标；全部写请求 `attachClientRequestId:false`（含 H-26）。`last_modified_at` 统一归一为 number，输出 `tableCount/myRole/starred/lastModifiedAt/lastModifier`。
- query key：`homeKeys.aggregate/search/searchScope`、`baseKeys.detail`，`query-keys/index.ts` 导出；写成功后失效聚合，搜索仅在激活时按 `homeKeys.searchScope()` 失效。
- 权限门：`features/permissions/` 提供 `normalizeBaseRole`、`resolvePermission`（12 个动作 × 4 角色 × 内外部）、`usePermission`、`PermissionGate`；新建按钮与卡片菜单项均经权限门渲染。
- 首页 UI：四区顺序「星标 → 最近访问 → 我创建的 → 与我共享」，卡片展示名称/表数/本地化时间/修改人/星标；搜索 1–50 字调 H-22；卡片菜单重命名（1–50 校验）、复制、删除（「删除后不可恢复（P0）」二次确认）、星标；外部账号仅渲染「与我共享」且无新建/菜单；1001、5001 错误就地提示并保留输入。

**文件**

- 新增：`web/src/features/home/{HomePage.tsx,HomePage.module.css,api.ts,format.ts,api.test.ts,HomePage.test.tsx}`、`web/src/features/home/components/{BaseCard.tsx,BaseCard.module.css}`、`web/src/features/permissions/{index.ts,matrix.ts,matrix.test.ts,usePermission.ts,PermissionGate.tsx}`、`web/src/query-keys/{home.ts,base.ts}`。
- 修改：`web/src/app/router.tsx`、`web/src/query-keys/index.ts`、`web/src/app/{App.test.tsx,router.test.tsx}`、`web/src/features/auth/{LoginPage,Logout,RegisterPage}.test.tsx`（补 H-21 mock 与卡片导航）。
- 删除：`web/src/features/workspace/{HomePage.tsx,HomePage.module.css}`。

**测试**

- `npm run test`：45 文件 / 572 用例全绿（基线 533，新增 39）。
- `npm run build`：TypeScript + Vite 构建成功，首屏 chunk 5 个（≤6）。

**偏差与契约缺口**

- 外部账号首页按需求 §3.1「仅与我共享」只渲染共享区（票面四区描述针对内部用户）；权限矩阵额外把 `star/manage_base/import/manage_structure` 对内部非管理者/外部恒隐藏。
- 无 `GET /bases/{id}`：本轮未接入 Base 详情，`baseKeys.detail` 仅作后续票 key 基座。
- H-21 的 `invitation_list` 与待办入口按 spec 延后 M4，未渲染。
