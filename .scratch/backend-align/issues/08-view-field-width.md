# 08: view-field-width（N4 视图级字段宽度）

**What to build:** 视图配置增「字段 id → 宽度（px）」映射，视图级存储；更新通道复用既有视图配置全量替换接口；跨会话生效；未设宽度用默认；非法值拒绝。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 写入宽度后读取一致，再次全量替换可覆盖
- [ ] 未设宽度回默认；负数或超大值被拒绝
- [ ] 不同视图的宽度配置互不影响
- [ ] proto 重生成脚本执行通过；往返测试全绿
- [ ] 文档护栏脚本全绿（RPC 计数保持 111）

## Answer
已落地（N4 视图级宽度）。`ViewConfig.field_width_map=7`（字段 id→px），宽度 40~2000、非法值/未知字段拒绝；视图级存储，复用 UpdateViewConfig 全量替换通道；未设宽度不出 key（前端默认渲染）；两视图隔离。Evidence: merge `d6d9c41`。