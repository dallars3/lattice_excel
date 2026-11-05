# 01: H-48 出参契约与空表网格修复

**What to build:** 新建的空 Base 在浏览器打开即呈现正常空态网格（幽灵行/新增入口），不再是错误横幅。后端游标查询契约补齐：空页恒返回 `record_list: []` 与 `has_more`/`next_cursor`；带 view_id 与不带的记录条目形状一致（`cells_map`/`version` 恒在，空值以空 map/null 表达）。前端游标解析保持严格抛错语义（tripwire 不放宽），MSW 补「真实后端空页形状」fixture 使组件测试能拦住此类回归。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 后端：H-48 空表（0 记录）返回 `{"record_list":[],"has_more":...,"next_cursor":...}`，契约测试锁定
- [ ] 后端：带 view_id 与不带的 H-48 条目均含 `cells_map`/`version`（带 view_id 时当前缺失，冒烟证据：`GET /api/v1/bases/{id}/records?table_id=X&view_id=Y` 条目无 cells_map/version；不带你 view_id 时正常），契约测试对比两种查询条目字段集一致
- [ ] 前端：MSW 新增「空页缺 record_list（真实后端形状）」fixture 用例，断言空表渲染空态而非错误横幅（`extractCursorPage` 严格语义保留，如因 fixture 对齐而不再触发抛错需说明）
- [ ] 前端：新建空 Base → 网格空态正常（组件测试 + 部署实例浏览器走查）
- [ ] `npm run test` 全绿、`npm run build` 成功、后端三服务契约测试通过、check-docs 全绿

**冒烟证据**：`.scratch/fullflow-test/test-cases.md` TC-050 系列；浏览器复现——新建 Base 打开即「操作失败，请稍后重试」横幅，`extractCursorPage` 抛「游标分页契约违背：响应缺少列表字段 record_list」；API 对照——空表 `GET .../records?table_id=` 返回 `data:{}`。
