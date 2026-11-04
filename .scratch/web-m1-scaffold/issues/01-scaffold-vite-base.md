# 01: 工程底座（Vite + React 18 + TS + 环境变量 + 构建约束）

**What to build:** `web/` 目录内一个从零 clone 即可一键安装、启动、构建、测试的前端工程：dev server 打开浏览器可见占位首页（含标题与试运行声明），构建产物成功产出且首屏 chunk 数 ≤6，目录与配置对齐前端技术方案第 2/4/16 章，运行时零外部服务依赖。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 依赖安装成功；`npm run dev` 启动并在浏览器渲染占位首页（标题 + 试运行声明文案）
- [ ] `npm run build` 成功产出 dist、无 TypeScript 类型错误；分包配置使首屏 chank 数 ≤6（HTTP/1.1 约束）
- [ ] 目录骨架、路径别名对齐技术方案分层（app/core/features/ui-kit/stores/query-keys/lib/styles）
- [ ] 环境变量 `VITE_API_BASE`/`VITE_WS_URL`/`VITE_NOTICE_TEXT` 定义且默认同源语义
- [ ] 无任何 CDN/外部字体/第三方运行时 SDK 引用；`.gitignore` 覆盖 `node_modules/`、`dist/`
- [ ] Vitest 最小配置可运行，基础用例一条全绿