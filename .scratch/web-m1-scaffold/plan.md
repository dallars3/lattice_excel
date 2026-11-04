# M1 前端工程搭建 — 执行计划

Status: ready-for-agent

事实源：[spec.md](spec.md)、《前端-技术方案-v0.1.md》第 2/3/4/5/6/8/11/12/15/16 章、《前端需求文档-v0.1.md》、[GLOSSARY.md](../../GLOSSARY.md)。本计划只安排执行顺序与落点，不新增设计决策。

## 0. Phase 0 — 环境前置（阻塞全部票）

| 项 | 现状 | 动作 |
|---|---|---|
| Node.js | ✅ 已完成（2026-11-05）：Node **v22.23.4**（v22 LTS 最新）+ npm 10.9.9 装于 `E:\node`（zip 免安装版）；用户 PATH 已追加 `E:\node`、`E:\node\npm-global`；npm prefix 定为 `E:\node\npm-global`（全局包装 E 盘）。新开终端生效，Git Bash 当前会话需 `export PATH="/e/node:/e/node/npm-global:$PATH"` | 无 |
| npm 网络 | 构建期依赖允许（spec 依赖治理） | `npm install` 走可达 registry；`package-lock.json` 提交 |
| Playwright 浏览器 | 未下载 | 票 09 时按需 `npx playwright install chromium`；仅配置阶段可跳过 |

Phase 0 不产出代码、不占票号；完成即开始票 01。

## 1. 依赖图（票面 Blocked by 汇总）

```text
01 工程底座
├── 02 app 装配与路由 ─────────────────┐
├── 03 token + ui-kit ─────────────────┤
├── 04 httpClient + MSW ───────────────┼──► 09 演示页 + Playwright + 全量收口
├── 05 realtimeClient（不阻塞 09）      │
├── 06 离线队列（不阻塞 09）            │
└── 07 grid 核心 ──► 08 渲染器注册表 ──┘
```

- 05/06 仅被 09 的最终回归（`npm run test` 全绿）覆盖，不进演示页链路。
- 并行可行性：01 完成后 {02, 03, 04}、{05, 06}、{07→08} 三条轨道互不依赖，可多 agent 并行；共享面见 §3 横切约定。

## 2. 执行顺序（单 agent 串行推荐）

```text
S0  Phase 0 环境前置
S1  票 01 工程底座
S2  票 03 token + ui-kit        ← 先于 02：HealthNotice/占位页直接消费 Banner 与 token
S3  票 02 app 装配与路由        ← providers 骨架先行，04/05/06 的模块有挂载点
S4  票 04 httpClient + MSW      ← client_request_id 生成器被 06 复用；MSW 供 09 复用
S5  票 05 realtimeClient        ← 只依赖 01，位置灵活；排 04 后因测试栈已在 S4 配平
S6  票 06 离线队列              ← 复用 04 的 client_request_id 与表驱动惯例
S7  票 07 grid 核心             ← 纯逻辑，随时可做；票面只依赖 01
S8  票 08 渲染器注册表          ← Blocked by 07
S9  票 09 演示页 + 收口         ← Blocked by 02/03/04/08（传递含 07）
```

每票完成即：勾选票面 checkbox → `git commit`（feature/p0-mvp 分支，建议 `feat(m1-NN): ...`）→ 进入下一票。收口前不留跨票未提交改动。

## 3. 横切约定（S1 定型，后续票遵守）

1. **依赖一次性装齐**：react18 / react-router-dom@6 / zustand / @tanstack/react-query / vite@5 / typescript@5 / vitest / jsdom / @testing-library/react / msw / @playwright/test 全部在票 01 安装并锁版本；03/04 只加各自的 setup 文件，不改 package.json——并行轨道不冲突。
2. **目录与命名**：严格按技术方案第 4 章目录（`src/app|core|features|ui-kit|stores|query-keys|lib|styles`）；术语沿用 GLOSSARY.md，不新造。
3. **测试风格**：Vitest 表驱动（沿用后端 Go 表驱动惯例）；MSW 是组件测试唯一 HTTP 边界；WS 一律 Fake transport 注入；纯逻辑直接单测。
4. **store 挂载点**：票 02 按技术方案第 8 章建 6 个 store 空骨架（selection/editSession/presence/offlineQueue/viewConfigDraft/ui）挂根；05/06 后续填充 offlineQueue 等实现时不回改装配层。
5. **query-keys 归属**：目录骨架建于票 01；`records` 游标 key 首版随票 04 落（游标分页适配即用）；其余域 key 常量 M2 按需补。
6. **围栏**：`.gitignore`（`web/node_modules/`、`web/dist/`）随票 01 提交；涉及 .md/.gitignore 变更后跑 `docs\tools\check-docs.ps1`。M1 不新增 README/设计稿（spec Out of Scope）。

## 4. 每票执行要点

### S1 票 01 工程底座
- 落点：`web/package.json`、`vite.config.ts`、`tsconfig*.json`、`index.html`、`src/app/main.tsx`、`src/styles/`、根 `.gitignore`、`src/vite-env.d.ts`。
- 要点：目录骨架按 §3.2；`VITE_API_BASE` 默认空串（同源）、`VITE_WS_URL` 默认按 `location` 推算、`VITE_NOTICE_TEXT` 默认文案；manualChunks 分组使首屏 chunk ≤6；Vitest 最小配置 + jsdom + 1 条冒烟用例；占位首页含标题与试运行声明。
- 验证：`npm install && npm run dev`（渲染占位页）、`npm run test`、`npm run build`（dist 产出、tsc 无错、chunk ≤6）。

### S2 票 03 token + ui-kit
- 落点：`src/styles/tokens.css`（色板/中性+品牌+状态四色+功能色、presence 10 色、间距、字号、三档行高、圆角、z-index 分级）、`[data-theme="dark"]` 空覆盖层；`src/ui-kit/{button,banner,toast,empty-state}`。
- 要点：token 全走 CSS 变量，抽样组件零魔法值（票面 AC）；Button 三类样式+禁用、Banner info/warning、Toast 入栈/自动消失/手动关闭、EmptyState 插图+文案+操作；各 ≥1 条 RTL 测试断言外部行为。
- 验证：`npm run test`（ui-kit 用例绿）。

### S3 票 02 app 装配与路由
- 落点：`src/app/router.tsx`、`src/app/providers.tsx`、`src/stores/`（6 个空骨架）、`src/features/health/HealthNotice`、三条占位页。
- 要点：`/login`、`/home`、`/base/:id` history 模式 + 整站回退（dev 由 Vite historyApiFallback 天然满足，刷新不白屏需验证）；QueryClient 挂根；HealthNotice 消费 `VITE_NOTICE_TEXT` 挂三处。
- 验证：1 条路由渲染冒烟 RTL 用例绿；手动刷新三条路由各一次。

### S4 票 04 httpClient + MSW
- 落点：`src/core/http/{client,error-catalog,idempotency,cursor}.ts`、`src/query-keys/records.ts`、`src/mocks/{server,handlers}.ts`。
- 要点：信封 `{code,message,data}`、`code===0` 透传、非 0 抛 `ApiError{code,message,meta}`；error-catalog 13 码（1001/1002/2001/2002/3001/3002/4001/4002/5001/5002/5003/9001/9002）三元组「文案/动作/恢复策略」+ 全局/局部双层消费标注；写请求 `client_request_id`（uuid）+ 重试复用（页内 map）；游标 helper 适配 `useInfiniteQuery`（cursor/limit/has_more/next_cursor）；MSW handler 工厂可对任意路由返回指定错误码。
- 验证：表驱动逐码断言三元组完整；幂等复用可测；游标集成用例走通。

### S5 票 05 realtimeClient
- 落点：`src/core/realtime/{state-machine,messages,transport,heartbeat}.ts`。
- 要点：五态 `idle → connecting → joined(snapshot) → reconnecting → revoked`（退避 1s 起上限 30s；revoked 不可逆）；心跳 15s、时钟/定时器注入；W-01~W-16 全部消息名类型化可枚举；`Transport` 接口 + `FakeTransport` + 生产 WS 占位；事件转类型化可订阅分发。
- 验证：五态迁移表驱动绿；心跳到期/应答用注入定时器测；全测试不触真实网络。

### S6 票 06 离线队列
- 落点：`src/core/offline/queue.ts` + `src/stores/offlineQueue.ts` 接线。
- 要点：状态迁移覆盖入队/断线只读（拒新编辑、在途保留）/重放（先补增量再顺序）/冲突转 4001 三动作（use_other/overwrite/copy）/失败项内容可复制不丢失/队首滞留 >5 分钟触发未同步清单提示（计时器注入）/store 实例隔离（多标签语义）。队列项 `client_request_id` 复用 S4 生成器。
- 验证：全状态迁移表驱动绿。M1 不接真网络（spec）。

### S7 票 07 grid 核心
- 落点：`src/core/grid/{windowing,row-height,frozen-columns,loaded-ranges,selection,keyboard}.ts`，全部 DOM 无关纯函数/状态机。
- 要点：窗口计算（三档行高估算 + 实测校准 + overscan 上下 5）；冻结列双通道划分；loadedRanges LRU（上限 ≈10 页）；SelectionModel（单元格/区域/行）；键位状态机（方向/Enter/Tab/Shift+Tab/F2/Esc/Delete/Ctrl+Z/C/V/X/F 纯函数，末行下键发新增信号）。帧预算 16ms 常量。
- 验证：表驱动覆盖顶部/中部/底部/分组行混合；校准收敛不跳行不重复；键位主路径表驱动绿。

### S8 票 08 渲染器注册表
- 落点：`src/core/grid/registry.ts`、`src/core/grid/renderers/{text,number,select}.ts(x)`、编辑器统一生命周期。
- 要点：register/query/去重、未知类型兜底；text 单行省略（全量值可获取）、number 千分位/小数按配置、select 色标；编辑器 `begin → input → validate → commit/reject`，Enter 提交/Tab 提交右移/Esc 取消三键一致。
- 验证：注册表单测 + 三渲染器 RTL 冒烟绿。

### S9 票 09 演示页 + 收口
- 落点：dev-only 演示路由（如 `/demo/grid`）+ `src/mocks/` 假数据游标 handler、`playwright.config.ts`。
- 要点：MSW 游标语义正确（cursor/limit/has_more）、滚动 fetchNextPage 加载新窗口；键盘「移动→编辑→提交/取消」全程；冻结列开关横向滚动固定区不动；三档行高即时切换；text/number/select 渲染可见；Playwright 配置可加载（无用例）。
- 收口：`npm run test` 全绿；`npm run build` 成功且首屏 chunk ≤6（可脚本化：build 后解析 dist chunk 计数）；全票 checkbox 勾选完毕后 M1 关闭。

## 5. 验收追溯（spec 覆盖底线 ↔ 票）

| spec 要求 | 票 |
|---|---|
| error-catalog 全 13 码映射 | 04 |
| 网格窗口计算边界（顶/中/底/分组） | 07 |
| 离线队列全状态迁移 | 06 |
| 键位状态机主路径 | 07 |
| HealthNotice 三处文案 | 02（文案来源 01 环境变量） |
| `npm test` 全绿 / build chunk ≤6 / dev 可启动 | 01 建立，09 收口 |

## 6. 风险与缓解

| 风险 | 缓解 |
|---|---|
| Node 未安装（已核实） | Phase 0 安装 LTS；未完成前不开工任何票 |
| chunk ≤6 与依赖数量冲突 | 票 01 就配 manualChunks 并验证，不留到 09 才发现 |
| MSW/Vite 版本与 Node 兼容 | 锁 Node 20/22 LTS + lockfile 提交 |
| 行高校准/LRU 边界用例设计成本高 | 用例清单先按票 07 AC 展开，表驱动一次写全 |
| 多标签队列隔离语义误实现为全局单例 | 票 06 用「两个 store 实例互不可见」用例锁死 |
| 并行轨道改同一文件 | §3.1 依赖集中 01、store 挂载点 02 定型，后续票不回改共享文件 |
