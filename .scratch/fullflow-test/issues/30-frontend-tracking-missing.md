# 前端未接对客埋点（H-94），关键事件不上报

Status: needs-triage
Severity: P2
Source: TC-180-03（UI）

## 复现步骤

1. 登录 → 新建/打开 Base → 编辑单元格。
2. 用 DevTools/Playwright 监听 `/api/v1/tracking/batch`。

## 实际

无任何埋点批量请求；`web/src` 检索 `tracking/batch`、`fe_` 均无实现。

## 期望

按附录 C / H-94：登录、建 Base、单元格提交等关键事件进入前端待上报队列并批量上报（`fe_login_success` / `fe_base_create` / `fe_cell_edit_submit` 等），服务端幂等接收。

## 证据

- `.scratch/fullflow-test/p1p2-ui-run.log` → `TC-180-03`
- 代码检索：前端无 tracking 模块

## 影响

运营指标与 `fe_perf_screen_ready` 等性能埋点全部缺失；TC-170-01 的埋点断言也无法满足。
