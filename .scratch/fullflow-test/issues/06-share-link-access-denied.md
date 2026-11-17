# 分享链接注册后无法进入 Base（服务端 3001，UI 落到 /home）

Status: needs-triage
Severity: P0
Source: TC-130-01（API+UI）

## 复现步骤

1. owner 对 Base 开启「任何人·可填写」链接，取得 `/s/{token}`。
2. 匿名访问 `/s/{token}` → 302 `/register?link_token={token}`。
3. 新访客完成注册（携带 link_token），随后访问该 Base：
   - UI：期望直接进入 `/base/{id}`；
   - API：用注册响应返回的 token（含 link_context）请求 `GET /api/v1/bases/{id}/tables`。

## 实际

- UI：注册成功后跳转 `/home`，未进入 Base。
- API：注册成功，但 `GET /bases/{id}/tables` 返回 `{"code":3001,"message":"角色无此操作权限"}`。

## 期望

按 TC-130-01 / AC-SHARE-03 / H-95：注册/登录后进入该 Base，链接角色（可填写）生效。

## 证据

- API 执行：`link=0, anon=302 /register?link_token=..., register=0, baseAccess=3001`
- UI 执行：注册后 URL 停在 `/register?link_token=...`（首轮 5003 限速），重试后 URL 为 `/home`。
- 结果文件：`.scratch/fullflow-test/p0-api-results.json`；Playwright `TC-130-01`。

## 影响

P0：分享链接主链路不可用；外部访客无法凭链接获得任何 Base 访问能力。需同时修复网关 CheckAccess 对 `principal.link_context` 的放行，以及前端链接注册后的落点（`/bases/{id}` 或 `/base/{id}` 路由对齐）。
