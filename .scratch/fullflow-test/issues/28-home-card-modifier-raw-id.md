# 首页 Base 卡片「最近修改人」展示内部 user_id

Status: needs-triage
Severity: P2
Source: TC-020-09（UI）

## 实际

首页卡片渲染为：

```
★ 2 个数据表  最近修改：2026/11/06 19:18 01M6R97EZYRB08AKQXS9T0BR1P
```

修改人位展示内部 user_id，而不是显示名/用户名。

## 期望

按 AC-HOME-01 / M2 票 02：卡片展示「最近修改时间与人」，人应为可读的显示名（缺失回退用户名）。

## 证据

- `.scratch/fullflow-test/p1p2-ui-run.log` → `TC-020-09`
- Playwright error-context 页面快照（星标区卡片文本）

## 影响

用户无法判断最近修改人，卡片信息失去可读性。
