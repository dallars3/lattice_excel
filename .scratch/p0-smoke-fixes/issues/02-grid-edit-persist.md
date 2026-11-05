# 02: 网格编辑提交落库与保存状态诚实化

**What to build:** 用户在网格编辑单元格并提交（Enter/Tab/失焦）后，数据真实落库（刷新页面与服务端读回一致），顶栏保存状态如实反映「保存中/已保存/保存失败」，失败时回滚乐观值并展示可解释文案（4001 冲突走既有三动作弹窗数据）。修复后「编辑 → 刷新 → 值还在」成立。

**Blocked by:** 01: H-48 出参契约与空表网格修复（同根因链：先保证 H-48 条目形状正确，再诊断编辑提交断点，验收才可信）

**Status:** ready-for-agent

- [ ] 诊断并修复编辑提交断点：编辑提交必发出 UpdateCell 请求（冒烟证据：合成事件与 CUA 真实键鼠双路径均零 PUT；`submitCell`/`trackWrite`/编辑器 onCommit 接线以实际断点为准，不臆测）
- [ ] 顶栏保存状态三态如实：保存中（写请求在途）/已保存（成功）/保存失败（失败并回滚乐观值）；禁止在未发出或失败时显示「已保存」
- [ ] 提交失败路径回归：1001 行内错误内容不丢；4001 进入冲突态（带 current_value/current_version）；4002/9001 各自文案
- [ ] 组件测试：GridArea 编辑提交用例（基于 01 的真实形状 fixture）断言 fetch 发出、成功更新、失败回滚；`npm run test` 全绿
- [ ] 部署实例浏览器走查：编辑单元格 → 刷新 → 值还在；`npm run build` 成功、check-docs 全绿

**冒烟证据**：编辑「第一行标题」+Enter → 零 PUT（performance entries 空）→ 服务端 `cells_map` 恒空（两条记录均 undefined）；部署 bundle 与源码构建 hash 一致（index.SBPH_vS4.js）→ 源码缺陷。
