# 07: 首页身份区与 P0 回归收口

**What to build:** 登录后首页顶栏可见通知铃铛与当前用户信息，通知未读数可读，登出入口归入用户菜单；随后以同一套黑盒用例完成全部 P0 回归，确认这批缺陷修复没有引入回归。

**Blocked by:** 01, 02, 03, 04, 05, 06

Status: done

- [x] 首页顶栏右侧显示通知铃铛，未读数来自未读计数接口，登录后拉取一次并按 30 秒刷新
- [x] 点击铃铛展示轻量浮层：未读数与「通知中心将在后续版本提供」，不出现白屏或报错
- [x] 顶栏显示当前用户显示名（缺失时回退用户名），退出登录位于用户信息菜单且行为不变
- [x] 未登录或接口失败时铃铛降级为 0 未读，不影响首页其它功能
- [x] TC-010-04 通过
- [x] 49 条 P0 全量回归全绿（API/WS + UI）
- [x] `contract-test.ps1` 与 `check-docs.ps1` 退出码均为 0
- [ ] 对 P1 主链路做抽样回归，无新增失败（spec §Out of Scope 已排除 P1/P2 用例，本轮以 P0 主链路 + 前端 1014 项单测替代）

## Comments

**改动**

- 新增 `web/src/features/notifications/api.ts`：`fetchUnreadCount` 消费 H-74，缺失/非法值按 0。
- 新增 `web/src/query-keys/notification.ts` 并在 `query-keys/index.ts` 导出未读计数 key。
- 新增 `web/src/features/home/components/NotificationBell.tsx(.module.css)`：铃铛 + 未读徽标 + 轻量浮层（未读数与「通知中心将在后续版本提供」，可关闭）。
- 新增 `web/src/features/home/components/UserMenu.tsx(.module.css)`：显示名（缺失回退用户名）触发菜单，退出登录移入菜单。
- `HomePage.tsx`：顶栏改为左标题、右身份区；登出仍复用 `logoutAndClear` 与 `/login` 跳转。
- 测试：扩展 `HomePage.test.tsx` 覆盖铃铛/未读数/浮层/30 秒刷新/接口失败降级/显示名回退/菜单登出；同步调整 `Logout.test.tsx` 先打开用户菜单。

**关键决策**

- 未读计数为旁路入口：`retry: false`、`refetchOnWindowFocus: false`，失败即 0，不拖慢首页其它请求。
- 刷新窗口固定 30 秒，页面隐藏时跳过轮询；`visibilitychange` 回到可见时立即补拉一次。
- 铃铛可访问名形如「通知，N 条未读」，TC-010-04 的 `/通知/` 可命中；用户菜单触发按钮可访问名为「用户菜单：<显示名>」。

**验证**

- `npm test` → 78 files / 1014 tests 全部通过。
- `npm run build`（tsc --noEmit + vite build）→ 退出码 0。

**待主代理执行**

- 49 条 P0 全量回归（API/WS + UI）、TC-010-04/TC-010-08 实跑、`contract-test.ps1`、P1 抽样回归：由主代理在集成分支部署后执行。
- `check-docs.ps1` 已在本地执行：退出码 0（127 个 md 文件，fail=0）；集成分支可再复跑。
- 注意：登出已移入用户菜单，TC-010-08 需先点击用户菜单再点「退出登录」；若 e2e 仍直接查找顶栏「退出登录」按钮需同步调整。

## Comments（主代理收口，2026-11-06）

- 部署态回归：`.scratch/fullflow-test/p0-api.mjs` **29/29 PASS**（含 TC-010-04 通知铃铛/用户信息）；`web/e2e/p0.spec.ts` **31/31 PASS**（TC-010-08 已改为先打开用户菜单再登出）。
- `contract-test.ps1` exit 0（core 14/14、worker 8/8、web 6/6，go vet 全绿）；`check-docs.ps1` exit 0（fail=0）。
- P1 抽样：spec §Out of Scope 明确排除 P1/P2 用例；以 P0 主链路全绿 + `npm test` 1014 项全绿作为回归证据，未发现新增失败。
