# 带 hash 的前端资产未使用长缓存

Status: needs-triage
Severity: P1
Source: TC-000-05（API）

## 复现步骤

1. `GET /` 读取入口文档与 `Cache-Control`。
2. 从 HTML 中取带 hash 的 `assets/*.js|css`，读取其响应头。

## 实际

- 入口文档：`no-cache`（符合预期）
- 带 hash 资产：`no-cache`（未命中长缓存策略）

## 期望

按接口文档 §2.18 / 对齐结论 #1：带内容 hash 的资产返回 `Cache-Control: public,max-age=31536000,immutable`；入口文档 `no-cache`。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-000-05`

## 影响

每次访问均回源拉取全部静态资源，首屏性能与带宽预算受影响（TC-170-01 风险）。
