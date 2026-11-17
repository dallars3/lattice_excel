# 伪造分享 token 被重定向到注册页而非失效页

Status: needs-triage
Severity: P1
Source: TC-000-06（API/入口）

## 复现步骤

1. 未登录访问 `GET /s/forged-token-xyz`（不存在/已失效 token）。

## 实际

`302 Location: /register?link_token=forged-token-xyz`

## 期望

按 H-95：失效 token 应 `302 /err?kind=share-link-unavailable`，展示「分享链接不可用」。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-000-06`

## 影响

用户会被引导注册/登录后才在 Base 访问阶段失败，入口三态（注册/进 Base/失效）不成立；也掩盖了链接被重置/关闭的事实。
