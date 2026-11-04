# 09: 网格演示页集成 + Playwright 配置 + 全量回归

**What to build:** dev 专用的网格演示页：MSW 模拟游标查询返回假数据，滚动按窗口加载新页；键盘导航/编辑/取消可实际操作；冻结列与三档行高切换即时生效；注册表演示渲染可见。同期完成 Playwright 安装与基础配置（不写用例）。最终以「npm test 全绿 + build 成功且首屏 chunk ≤6」收口 M1。

**Blocked by:** 02: app 装配与路由骨架，03: design token 与 ui-kit，04: httpClient 层，08: CellRenderer/CellEditor 注册表

**Status:** ready-for-human

- [x] dev 下访问演示页显示假数据网格，滚动触发新窗口加载（MSW 的 cursor/limit/has_more 语义正确）
- [x] 键盘完成「移动选中 → 进入编辑 → 提交/取消」全程可操作（对应键位表）
- [x] 开启冻结列后横向滚动，固定区不随滚动；行高紧凑/中等/高三档切换即时生效
- [x] 演示页可见三个演示渲染器（text/number/select）的正确渲染
- [x] Playwright 已安装并出基础配置，可启动（无用例要求）
- [x] 全量收口：`npm run test` 全绿；`npm run build` 成功且首屏 chunk ≤6

## Comments

- feat commit：`02b74a5`（分支 feature/m1-web）。
- 落点：`web/src/features/grid-view/`（GridDemoPage 演示页 + demo-table 数据面 + useDemoRecords 游标接线 + RTL 用例 + CSS Modules）+ `web/src/app/router.tsx`（/demo/grid 注册）+ `web/src/app/main.tsx`（DEV 门动态 import MSW worker）+ `web/src/mocks/browser.ts`（setupWorker）+ `web/src/mocks/mockServiceWorker.js`（SW 脚本，置 src/mocks 不进 public/）+ `web/vite.config.ts`（dev-only SW 中间件 + vitest 排除 e2e）+ `web/playwright.config.ts` + `web/e2e/`（占位 spec）。零依赖变更（package.json/lockfile 未动）。
- 实现要点：
  - **演示页 = grid 核心消费证明**：windowing 纵向虚拟窗口（overscan 默认 5）、frozen-columns sticky 双通道（固定区 sticky、滚动区横向虚拟化窗口，列 overscan 取 2 便于观测窗口推进）、loaded-ranges LRU 页缓存（状态栏可观测「已加载 N 行 · 缓存 N 页 · LRU 淘汰 N 页」）、registry 渲染器/编辑器经 registerDemoCells 注册、RowHeightModel 三档。
  - **游标数据链路**：useInfiniteQuery + 票 04 helper（extractCursorPage/getNextPageParam）+ recordsKeys.list；fetchNextPage 仅由滚动事件触发（距已加载底部 ≤5 行预取，首屏不自动追页），cursor 按 next_cursor 链推进（用例断言 null→cur_20→cur_40 且 has_more=false 终止）。
  - **键盘全程**：容器 keydown → applyKey 迁移 + 副作用信号消费（Delete 清空落地；appendRow/clipboard/undo/find 为 M1 信号位随 M2）；编辑态三键归注册表编辑器（resolveEditorKey），onCommit 后按提交移动语义还原键事件回状态机取迁移目标态（焦点推进单一裁决点）；Esc 取消不落盘（AC-REC-02）。
  - **冻结列 sticky 双通道**：固定区 `position: sticky`（内联 left=通道偏移、z-index 走 token、top/bottom 置 auto 防纵向钉死），滚动区按内容坐标定位随滚动平移；列头与主体同构双通道（scrollLeft 反向平移对齐）。
  - **MSW 只进 dev**：main.tsx `import.meta.env.DEV` 动态 import → 构建期静态消除（build 输出无 msw chunk）；SW 脚本由 `apply:'serve'` 中间件按需服务、不入 public/（dist 无 mockServiceWorker.js）；组件测试走 msw/node（setup.ts），dev worker 与测试共用同一数据面（demo-table）保证 cursor/limit/has_more 语义一致。
  - **Playwright**：配置 testDir e2e + list reporter + baseURL，默认不起 webServer、不下载浏览器；空 testDir 会以 "No tests found" 非零退出，故落 1 条 test.fixme 占位使 `--list` 以 0 实跑用例正常退出（真实用例随 M5 收口）；vitest exclude e2e（@playwright/test 的 spec 不能被 vitest 收集）。
- 验证：`npm run test` **481/481 绿**（基线 470 不回归，新增 11：演示页 10 + 路由 1）；`npx tsc --noEmit` 0 错；`npm run build` 成功，首屏 JS chunk **4** ≤6（index / vendor-react / vendor-router / vendor-query），产物无 msw chunk、dist 无 SW 文件；`npx playwright test --list` 退出码 0；dev server（后台启动、验证后已杀进程）：curl `/demo/grid` 200 返回 SPA 入口、`/mockServiceWorker.js` 200 text/javascript、主站三条路由 200；真实浏览器端到端：假数据 20 行渲染（渲染器千分位/色标/省略 title 可见）、滚动追页 20→40→60（缓存 1→2→3 页）、键盘「点击选中→F2→输入→Enter 提交下移→Esc 取消不落盘」全程可操作、冻结列横向滚动钉住视口左缘（sticky 实测）、三档行高实测 32/40/48px。
- 偏差：
  - 演示页路由恒注册（生产可访问），但 MSW worker 仅 dev 启用——生产访问 /demo/grid 呈 error-catalog 错误态横幅（页内注释已锚定 M1 定位）；「dev 专属」语义按票面要求落在数据来源而非路由开关。
  - 编辑提交与 Delete 清空均为演示数据本地覆盖层，真实记录写入随 M2 服务接入（键位状态机信号位注释锚定）。
  - Playwright 占位 spec 为达成「0 用例正常退出」验收语义所加（空 testDir 非零退出）；未安装浏览器、未写真实用例（M5 收口项）。
  - 真实浏览器验证中发现并修复一处键盘可用性缺陷：单元格/编辑器非可聚焦元素，提交/取消/点击后焦点显式归还网格视口（RTL 直发事件测不出该缺陷，已补焦点断言回归）。
