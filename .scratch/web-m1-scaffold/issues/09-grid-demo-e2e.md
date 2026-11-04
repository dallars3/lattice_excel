# 09: 网格演示页集成 + Playwright 配置 + 全量回归

**What to build:** dev 专用的网格演示页：MSW 模拟游标查询返回假数据，滚动按窗口加载新页；键盘导航/编辑/取消可实际操作；冻结列与三档行高切换即时生效；注册表演示渲染可见。同期完成 Playwright 安装与基础配置（不写用例）。最终以「npm test 全绿 + build 成功且首屏 chunk ≤6」收口 M1。

**Blocked by:** 02: app 装配与路由骨架，03: design token 与 ui-kit，04: httpClient 层，08: CellRenderer/CellEditor 注册表

**Status:** ready-for-agent

- [ ] dev 下访问演示页显示假数据网格，滚动触发新窗口加载（MSW 的 cursor/limit/has_more 语义正确）
- [ ] 键盘完成「移动选中 → 进入编辑 → 提交/取消」全程可操作（对应键位表）
- [ ] 开启冻结列后横向滚动，固定区不随滚动；行高紧凑/中等/高三档切换即时生效
- [ ] 演示页可见三个演示渲染器（text/number/select）的正确渲染
- [ ] Playwright 已安装并出基础配置，可启动（无用例要求）
- [ ] 全量收口：`npm run test` 全绿；`npm run build` 成功且首屏 chunk ≤6