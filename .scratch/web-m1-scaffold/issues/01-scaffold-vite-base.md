# 01: 工程底座（Vite + React 18 + TS + 环境变量 + 构建约束）

**What to build:** `web/` 目录内一个从零 clone 即可一键安装、启动、构建、测试的前端工程：dev server 打开浏览器可见占位首页（含标题与试运行声明），构建产物成功产出且首屏 chunk 数 ≤6，目录与配置对齐前端技术方案第 2/4/16 章，运行时零外部服务依赖。

**Blocked by:** None (can start immediately)

**Status:** ready-for-human

- [x] 依赖安装成功；`npm run dev` 启动并在浏览器渲染占位首页（标题 + 试运行声明文案）
- [x] `npm run build` 成功产出 dist、无 TypeScript 类型错误；分包配置使首屏 chank 数 ≤6（HTTP/1.1 约束）
- [x] 目录骨架、路径别名对齐技术方案分层（app/core/features/ui-kit/stores/query-keys/lib/styles）
- [x] 环境变量 `VITE_API_BASE`/`VITE_WS_URL`/`VITE_NOTICE_TEXT` 定义且默认同源语义
- [x] 无任何 CDN/外部字体/第三方运行时 SDK 引用；`.gitignore` 覆盖 `node_modules/`、`dist/`
- [x] Vitest 最小配置可运行，基础用例一条全绿

## Comments

- 实现要点：
  - `web/` 手写工程（无交互式脚手架）：Vite 5.4.21 + React 18.3.1 + TS 5.9.3；`@` → `src` 别名在 vite.config.ts（resolve.alias）与 tsconfig.json（baseUrl+paths）双侧对齐；build 脚本为 `tsc --noEmit && vite build`。
  - 目录骨架按技术方案第 4 章建齐：`app` / `core`（http·realtime·grid·offline）/ `features` / `ui-kit` / `stores` / `query-keys` / `lib` / `styles`；各占位 `index.ts` 注释了后续落地票（04/05/06/07/08/02/03）。
  - 环境变量 `src/lib/env.ts`：`API_BASE` 默认 `''`（同源）、`WS_URL` 默认按 `location.protocol` 推算 `ws://`/`wss://` 同源、`NOTICE_TEXT` 默认引用《前端需求文档-v0.1.md》3.1 原文「个人试运行产品，不保证服务持续与数据留存，重要数据请自行导出备份」；未建 .env 文件；纯函数 `resolveEnv` 供表驱动单测。
  - manualChunks 采用函数式分组（vendor-react / vendor-router / vendor-query / vendor-state / vendor），仅对进入构建图的依赖产生 chunk，不产空文件。
  - 样式：`styles/tokens.css` 骨架（间距/字号/三档行高/圆角/z-index/少量色板，完整 token 清单归票 03）+ `global.css`（系统字体）；占位首页用 CSS Modules，取值全走 `var(--token)`。
  - `main.tsx` 仅挂 `App`（路由与 QueryClient/realtimeClient 留票 02 接入）。
- 依赖（计划 §3.1 一次装齐，package-lock.json 已提交）：react 18.3.1、react-dom 18.3.1、react-router-dom 6.30.6、zustand 5.0.16、@tanstack/react-query 5.106.0、vite 5.4.21、@vitejs/plugin-react 4.7.0、typescript 5.9.3、vitest 3.2.7、jsdom 30.1.3、@testing-library/react 16.3.3（+dom 10.4.2）、@testing-library/jest-dom 6.9.1、msw 2.15.0、@playwright/test 1.64.1（浏览器下载后置到票 09）。
  - 偏差 1：@testing-library/jest-dom 用 `~6.9.1` 而非 `^6`（锁定 6.9.x）：npm 明示 6.10.0 为「错误发布的次版本、含破坏性变更」，6.9.1 才是 6.x 正确版本；仍满足票面 `@^6` 大版本约束。
  - 已知风险：npm audit 7 项（4 moderate / 1 high / 2 critical）全部位于锁定大版本的构建/测试工具链（vitest 3 → tinypool、@vitest/mocker；vite 5 → esbuild dev server；react-router 6 → Link 开放重定向等），修复需跨大版本（vitest 5 / vite 8 / react-router 7），违反技术栈锁定不予升版；均为 dev-only 或本应用不含的用法（无 SSR、M1 无对外 Link 目标、dev server 仅 localhost），票 09 收口时复查。
  - 偏差 2（围栏共修）：`docs/tools/check-docs.ps1` 原先递归扫描全部 .md，`web/node_modules/` 内第三方依赖文档（LF 行尾）导致护栏必然失败、pre-commit 永久拦截；按 AGENTS.md「失败先修再继续」在两处递归扫描增加 `node_modules` 路径排除（仓库自有 98 个 md 全过，exit 0）。
- 验证（web/ 内真实执行）：
  - `npm install`：219 包安装成功（Node v22.23.4 / npm 10.9.9）。
  - `npm run test`：8/8 全绿（env 表驱动 7 条 + App RTL 冒烟 1 条）；TDD 红先行已确认（实现文件缺位时两个测试文件均失败，随后 env.ts → 绿 7/7，App.tsx → 绿 8/8）。
  - `npm run build`：`tsc --noEmit` 无类型错误；dist 产出；首屏并发 chunk = **3**（index.html 引用 entry js + vendor-react js + css，≤6）。
  - `npm run dev`：后台启动后 `curl http://localhost:5173/` 返回含 `<title>协作多维表格</title>` 的首页 HTML；`/src/lib/env.ts` 转换产物含默认试运行文案（渲染层由 App RTL 用例佐证）；验证后进程已终止。
- Commit: `61ec01df125882a913a0685532a4ba8189c929d3`（feat(m1-01)，含 .gitignore 与 check-docs.ps1 围栏共修；本票文件勾选/回填为后续单独 docs 提交——hash 无法自引用，见偏差 3）
  - 偏差 3：票内回填 commit hash 与「单提交包含票文件」互斥（hash 在提交后才存在），故拆为 feat 提交（代码+围栏）+ docs 提交（本文件）。