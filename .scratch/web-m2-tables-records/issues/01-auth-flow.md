# 01: 认证闭环（注册·登录·登出·会话守卫）

**What to build:** 让浏览器里的真实用户能完成注册与登录并拿到会话：直接建企业的普通注册、凭企业邀请的注册、凭分享链接的注册三条路径都要可用且有差异化文案；登录成功进入首页（带 next 参数时回到原页面）；任意接口返回会话失效时统一弹重登并以原页面为返回目标；登出后清空会话与本地缓存。登录页、注册页、首页固定展示试运行免责声明。

**Blocked by:** None（可立即开始）

**Status:** resolved

- [x] 注册表单校验用户名（4–20 位、字母开头、字母/数字/下划线）与密码（8–32 位、须含字母与数字），非法时不发请求并给出字段级文案
- [x] 企业名称为可选输入，且仅在既无邀请也无 link_token 时出现；三条路径各有差异化归属提示
- [x] 服务端 1002（用户名已存在）与 1001（参数非法）以行内错误呈现，已填内容不丢失
- [x] 登录成功写入同源 Cookie 会话并跳转首页；存在 next 参数时返回该路径
- [x] 登录失败按 2002 显示锁定/失败提示，并固定展示「如遗忘密码，账号将无法使用」，页面不提供找回入口
- [x] 任意接口回包 2001 时弹重登弹窗，重登成功后回到原页面；登出清空服务端状态缓存与编辑态
- [x] 登录/注册/首页三处均渲染同一条试运行声明文案
- [x] 组件测试经 MSW 覆盖 1001/1002/2001/2002 四条路径与三条注册路径的文案差异

## Answer

认证闭环已按票面落地，前端全部经既有 `httpClient` 消费 H-01~H-04：

- 传输层：`web/src/core/http/client.ts` 新增 `RequestOptions.attachClientRequestId`（默认 true，H-01/H-02/H-03 传 false）与 `setApiErrorObserver`（每次 ApiError 抛出前通知，观察者故障不影响原错误）；`core/http/index.ts` 导出类型。
- 认证域：`web/src/features/auth/api.ts` 提供注册/登录/登出/me，并封装 `loginAndLoadMe`、`registerAndSignIn`（注册 → 补登录换 Cookie → me）。`types.ts` 按网关 protojson（UseProtoNames=true，snake_case / int64 字符串）建模。
- 会话：`web/src/stores/auth.ts` 的 `AuthSession` 由 `enterprise` 或 `user.enterprise_id` 派生 `internal`；提供 setSession/clear/openRelogin/closeRelogin，`stores/index.ts` 已导出。
- 页面：`RegisterPage`（三条路径 `?invite=1`/`?invite_id=`、`?link_token=`、无参数；企业名仅建企业路径可见；字段级校验 + 1001/1002 行内错误且保留输入）、`LoginPage`（next 仅接受 `/` 开头且非 `//`；2002 固定提示与「如遗忘密码，账号将无法使用」，无找回入口）、`LoginForm`（登录页与重登弹窗复用）。
- 守卫与重登：`RequireSession` 覆盖 `/home`、`/base/:id`，无会话探测 H-04，失败跳 `/login?next=<pathname+search>`；`ReloginModal` 由 `providers.tsx` 的全局桥在任意 2001 时打开，重登成功关闭并失效 me 查询、留在原页面。
- 登出：`logout.ts` 调 H-03 后清 `queryClient.clear()`、authStore、editSession/selection/presence/viewConfigDraft/uiStore，回 `/login`；首页提供「退出登录」入口。
- 路由：新增 `/register`；`/login`、`/register`、`/home` 均挂 `HealthNotice`，`/home` 标题「协作多维表格」保持不变。

测试与构建（`web/`）：

- `npm run test`：42 个文件 / 533 用例全绿（基线 481 + 本票新增 52）。
- `npm run build`：`tsc --noEmit` + vite build 成功（首屏 chunk 5 个，含 vendor 分包）。

契约缺口与前端兜底：

1. `POST /api/v1/auth/register` 不设置会话 Cookie（`register.go` 仅回 token），前端在注册成功后自动调用 H-02 登录换取 Cookie，再拉 H-04 me 作为身份事实源。
2. H-01 请求体没有邀请参数（仅 `username/password/enterprise_name/link_token`）；`?invite=`/`?invite_id=` 仅用于文案差异化，邀请归属由服务端按账号/服务端状态判定，前端不上送邀请 ID。
3. 后端严格 JSON（`DisallowUnknownFields`）不接受 `client_request_id`，故 H-01/H-02/H-03 全部关闭自动幂等字段；H-03 请求体为空。
4. 登出后 RequireSession 已挂载的 me observer 可能被 React Query 重建为一个空 query（无数据残留）；身份数据与业务缓存均已清空。
5. G-01-1：H-01 无邀请参数，`?invite=`/`?invite_id=` 实际也走「新建企业」；企业名留空时统一上送 `{username}的企业`，链接注册仍只发 `link_token`。
