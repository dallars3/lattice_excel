# 17: 交付与可观测（静态缓存 / 埋点）

**What to build:** 带内容 hash 的前端资产返回长缓存，入口文档保持 no-cache；前端接入 H-94 埋点，登录、建 Base、单元格提交、首屏可交互等关键事件批量上报，失败静默不重试放大。对应缺陷 #21、#30。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 带 hash 的 `assets/*` 返回 `public,max-age=31536000,immutable`；入口文档与无 hash 资源为 `no-cache`。
- [ ] 登录/建 Base/编辑各触发一次批量上报，`event_name` 符合附录 C（如 `fe_login_success`、`fe_base_create`、`fe_cell_edit_submit`）。
- [ ] 首屏可交互后上报 `fe_perf_screen_ready`，携带 rows 与耗时。
- [ ] 上报失败静默丢弃，不重试放大，不影响主流程。
