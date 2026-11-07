# 17: 交付与可观测（静态缓存 / 埋点）

**What to build:** 带内容 hash 的前端资产返回长缓存，入口文档保持 no-cache；前端接入 H-94 埋点，登录、建 Base、单元格提交、首屏可交互等关键事件批量上报，失败静默不重试放大。对应缺陷 #21、#30。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] 带 hash 的 `assets/*` 返回 `public,max-age=31536000,immutable`；入口文档与无 hash 资源为 `no-cache`。
- [x] 登录/建 Base/编辑各触发一次批量上报，`event_name` 符合附录 C（如 `fe_login_success`、`fe_base_create`、`fe_cell_edit_submit`）。
- [x] 首屏可交互后上报 `fe_perf_screen_ready`，携带 rows 与耗时。
- [x] 上报失败静默丢弃，不重试放大，不影响主流程。

## Comments

已收口。静态缓存正则修正（真实 Vite 混合大小写 hash 曾全部漏判——缺陷 #21 实际未修的真相）+ tracker 批量上报（附录 C 事件名，fe_perf_screen_ready 含 rows/t_ms）+ 失败静默丢弃；TC-000-05 绿；contract-test 全绿。
