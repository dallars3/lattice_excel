# 02: app 装配与路由骨架（providers + HealthNotice）

**What to build:** 应用骨架装配：登录/首页/Base 工作台三条占位路由可导航、刷新不白屏；QueryClient 与 Zustand stores 挂载于根；试运行免责声明组件（HealthNotice）在登录、注册/首页、Base 三处强制渲染。此后各里程碑功能落地于该骨架之上。

**Blocked by:** 01: 工程底座

**Status:** ready-for-human

- [x] `/login`、`/home`、`/base/{id}` 三条路由可达、可互相导航，history 模式整站回退可用
- [x] 任意路由直接刷新不白屏（占位内容保持在位）
- [x] providers 装配完成：QueryClient 挂根；声明的 Zustand stores 可被任意组件消费到初始状态
- [x] HealthNotice 组件在登录页、首页、Base 工作台三处渲染试运行免责声明（文案来自环境变量）
- [x] 装配层可测试（一条路由渲染冒烟用例绿）

## Comments

- 实现（feat `1f98699`，仅动 web/src）：
  - `src/app/providers.tsx`：QueryClient（@tanstack/react-query）挂根；6 个 Zustand store 为模块级单例，无需 Provider 即全树可达（plan §3.4 挂载点，票 05/06 填实现时不回改装配层）。
  - `src/app/router.tsx`：`/login`、`/home`、`/base/:id` 三条占位路由（MemoryRouter 包裹冒烟），另含 `/` 与未知路径回退 `/home`；history 模式（App.tsx 内 BrowserRouter）。App.tsx 改为装配根；票 01 冒烟用例同步调整为 /home 路由下渲染（断言不变）。
  - `src/features/health/HealthNotice.tsx`：文案取 `env.NOTICE_TEXT`（VITE_NOTICE_TEXT，默认 DEFAULT_NOTICE_TEXT），视觉复用 ui-kit Banner（warning 语义）；登录页、首页、Base 工作台三处强制挂载（技术方案第 14 章）。
  - `src/stores/`：技术方案第 8 章 6 个 store 空骨架（selection/editSession/presence/offlineQueue/viewConfigDraft/ui），各含最小初始 state 与类型；不实现业务逻辑（offlineQueue 状态机=票 06，realtime=票 05，selection/键位=票 07）。
  - 三占位页（auth/LoginPage、workspace/HomePage、workspace/BaseWorkspacePage）带互导航链接；标题与试运行声明并入 /home；样式全部走 tokens.css 变量。
- 验证：`npm run test` 126/126 绿（既有 102 无回归 + 新增 24：HealthNotice 2、路由冒烟 8、store/装配探针 14）；`npm run build` 成功（tsc 无错），首屏 JS chunk 4 个（index/vendor-react/vendor-router/vendor-query）≤6；`npm run dev` 后台启动验证 `/login`、`/home`、`/base/base_001` 直接请求均 200 返回 SPA 入口（Vite historyApiFallback，刷新不白屏），验证后 PowerShell Stop-Process 清理（pid 34196）。
- 说明：build 产物整站回退（history 路由统一回 index.html）为后端静态托管项（技术方案第 16 章、对齐结论 #1），不在本票范围；dev 已天然满足刷新不白屏。
- 偏差：`@testing-library/user-event` 未随票 01 安装且本票禁改 package.json/lockfile，导航冒烟改用 `fireEvent.click` 驱动，行为等价；React Router v7 future-flag 提示为 v6 无害 stderr 噪音，未引入行为变更。