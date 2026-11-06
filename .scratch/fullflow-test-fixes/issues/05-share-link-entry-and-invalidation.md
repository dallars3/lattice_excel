# 05: 分享链接访客进入与链接失效

**What to build:** 持分享链接的新访客注册或登录后直接进入对应 Base，并能以链接角色读取数据表、字段、视图与记录；重置或关闭链接后旧 token 立即失效并进入失效页，新链接继续可用。

**Blocked by:** None (can start immediately)

Status: done

- [x] 匿名访问 `/s/{token}` 跳转注册页并携带 link_token；注册成功后直接进入该 Base 工作台
- [x] 已登录访客访问 `/s/{token}` 直接进入该 Base；落点与前端规范路由 `/base/{id}` 对齐
- [x] 无企业归属的链接会话可读取该 Base 的数据表、字段、视图与记录查询，链接角色（可查看/可填写）生效
- [x] 链接会话访问其它 Base 仍被拒绝；结构写操作仍要求企业成员或可管理角色
- [x] 失效 token 进入 `/err?kind=share-link-unavailable` 失效页，不再回退首页
- [x] 重置链接后旧 token 立即失效；关闭链接与管理员强制关闭同样立即失效
- [x] 网关链接缓存的键包含 token 与链接角色；重置/关闭后旧 token 缓存被清除
- [x] 新链接继续可用；链接注册用户重新登录后仍可访问该 Base（链接未失效时）
- [ ] TC-130-01/02 通过；外部协作者导出仍返回 3002
- [x] 接口文档同步 H-95 落点与链接关闭事件载荷；补 ADR 记录外部链接账号的租户边界

## Comments

### 根因

1. 结构域 `memberPrincipal` 用 `principal.enterprise_id` 查企业角色，链接注册的外部账号企业为空 → 所有只读结构接口返回 3001。
2. 网关 `Cache.Access` 的键只有 `user:base:action`，外部链接会话与普通会话、不同链接角色共用鉴权缓存。
3. 网关 `shareLinkBasePrefix` 仍是旧的 `/bases/`，与前端规范路由 `/base/:id` 不一致；注册页链接路径成功后固定 `/home`；无 `/err` 路由。
4. `link.closed` 载荷不含 token，网关无法按 token 失效；`/s/{token}` 缓存未命中时仅比对账号上残留的 LinkContext，重置后仍能跳 Base。
5. 重置/关闭网关侧不主动清缓存，旧链最长存活 55s。

### 改动

- core/structure：新增 `readPrincipal`（无企业归属不要求成员角色）、`tablesInBaseForRead`/`tableForReadTenancy`（外部账号按 Base 企业口径、不带企业过滤），只读四接口改走新 seam；写接口保持 `memberPrincipal`/`managerPrincipal` 不放宽；`base.StructureRepository.TablesByBaseCollaborator`。
- core/share：`link.closed` 载荷新增 `token`，重置携带轮换前旧 token，关闭/强关/收窄携带当前 token。
- core/iam：补充链接注册用户重新登录后仍携带持久化 LinkContext 的回归断言（实现本身已正确）。
- web_api/redis：`Access` 增加 `linkScope`（token+角色）参与缓存键；新增 `DeleteShareLinkBase`；`AuthInvalidator` 订阅 `link.closed` 时按 token 清缓存。
- web_api/gateway：`/s/{token}` 落点改 `/base/{base_id}`，缓存未命中回源 `share.CheckAccess` 校验；reset/close 先 `GetLink` 取旧 token，成功后删除缓存（失败仅告警）；`linkCacheScope` 把链接上下文折进鉴权缓存键。
- web：注册页链接路径成功后 `window.location.assign('/s/{token}')`；新增 `/err` 路由与 `ErrorPage`（`kind=share-link-unavailable` 展示失效页）。
- docs：接口文档 v0.7 的 H-95 落点、校验口径与 E-08 载荷；前后端对齐结论 v0.2 N6；新增 `docs/adr/0004-external-link-account-tenancy.md`。

### 验证

- `excel_core_service`：`go test -p 1 ./...` 全绿（share/structure/iam 等 13 包）。
- `excel_web_api`：`go test -p 1 ./...` 全绿（gateway/redis/realtime 等）。
- `web`：`npm run build` 通过；`npm test` 78 文件 / 1009 用例全绿。
- 文档：`powershell -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1` 退出码 0。
- TC-130-01/02 的黑盒验收需部署后由 P0 冒烟脚本执行；本 ticket 已用包级 seam 覆盖同一行为（链接三分支、结构只读、重置/关闭/强关失效、重新登录）。

### 补丁：表级/视图级只读路由的 Base 授权兜底

- 缺口：网关仅对带 `{base_id}` 的路由跑 BaseAuth；`/tables/{table_id}/fields`、`/tables/{table_id}/views`、`/views/{view_id}/config` 是 `SessionRequired`，结构域此前只放开企业成员校验而未做 Base 授权，外部账号可凭 ID 读其它 Base。
- 修复：结构域新增窄接口 `ReadAccess`（`AllowRead(ctx, principal, baseID)`）与适配器 `share_read_access.go`（`NewShareReadAccess(share.Service)` → `share.CheckAccess`，`ACTION_VIEW`，携带 `LinkContext`）；`NewService` 增加该依赖并注入 `cmd/main.go`。
- 只读入口解析出 Base 后统一走 `authorizeRead`：ListTables 用 `base_id`；ListFields/ListViews 用表行 `BaseID`；GetViewConfig 经 view→table 取 `BaseID`。内部成员仍走 `memberPrincipal`，不调用该 seam。
- 失败口径：seam 拒绝或未装配时外部账号 fail-closed 返回 3001；新增包级用例覆盖未授权、seam 缺失、已授权与内部成员不回归。
- ADR `0004` 已同步：网关 BaseAuth 覆盖 base 级路由，表级/视图级路由由 core 结构域用同一 CheckAccess seam 兜底。
