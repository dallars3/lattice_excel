# 06: 网格停滞 trace 与重复稳定性收口

**What to build:** 在 live stack 上重复执行排序、筛选、分组切换与写入压力场景，默认在失败时保留可诊断 trace。trace 必须区分 H-47 未完成、H-48 未启动、H-48 未返回和响应已返回但 UI 未提交。05 的竞态修复必须经过重复回归；若 trace 暴露剩余卡点，在本票内修正并添加回归断言。

**Blocked by:** 05: 视图配置确认态与记录查询竞态修复.

**Status:** done

- [x] Playwright 网格停滞专项启用 retain-on-failure trace，并在失败产物中包含 ViewConfig 保存结果、Record 查询 key、请求起止、响应与 UI 提交时序
- [x] 专项场景覆盖连续排序、筛选、分组切换及写后浏览，断言最终数据对应当前已确认 ViewConfig
- [x] sf-stress 六条用例至少连续运行 7 轮无失败，任何一次失败都保留可直接定位请求阶段的 trace
- [x] 修复后的组件级延迟/乱序回归用例继续通过，证明 05 的“先确认配置、再查询记录”顺序未退化
- [x] 若 live trace 揭示 05 之外的卡点，新增最小复现测试、完成定向修复并重复专项运行直到无 120 秒级停滞
- [x] 不通过增加固定 sleep、放宽最终数据断言或无限重试规避停滞

## Comments

**2026-11-20 实现与验证：**

- 实现提交 `5d8f310`：新增 `web/e2e/grid-stall.spec.ts` 与浏览器侧诊断探针，文件级启用 `trace: retain-on-failure`。失败时同时保留 `trace.zip` 和 `grid-stall-diagnostics.json`，附件含 H-47 结果、确认/预期 query key、H-48 起止/响应、预期/当前 UI、网络时序及失败阶段。
- 阶段分类规则覆盖 `H47_NOT_COMPLETE`、`H48_NOT_STARTED`、`H48_STARTED_NO_RESPONSE`、`H48_RESPONSE_RECEIVED_UI_NOT_COMMITTED`。受控红测实际验证前三类中的 H-47 挂起、H-48 已发未回，以及 H-48 成功但 UI 未提交；H-48 未启动以同源分类规则锁定，无自然故障可复现。
- trace 首次揭示 05 之外的真实卡点：连续分组切换后虚拟分组行使用 `group-${key}`，非连续同名组产生重复 React key，旧组头残留并与最终状态组头混排。组件级最小回归“连续切换分组字段后只渲染最终确认字段的组头，不混入中间草稿分组”稳定复现；修复为包含虚拟 section index 的唯一 key。
- 专项最终断言使用 H-46 回读确认配置，并以当前 H-48 首窗口作为独立预期；UI 标题序列必须完全一致，分组只接受完整顺序中的连续渲染窗口切片，以覆盖真实虚拟化与写后焦点滚动。没有新增固定 sleep、放宽标题/顺序断言或无限重试。
- 专项连续 7 轮：`35 passed / 0 failed`（1.7m）。覆盖连续排序、连续筛选、连续分组、写后连续浏览、120 秒级停滞预算五条场景。
- sf-stress 六条原用例连续 7 轮：`42 passed / 0 failed`（6.8m）。仅增加文件级 `test.use({ trace: 'retain-on-failure' })`，六条用例内容未改；该配置复验单轮 `6/6 passed`（1.0m）。
- 目标 Vitest：`BaseWorkspacePage.views.test.tsx`、`GridArea.records.test.tsx`、`view-grouping.test.ts`、HTTP client 与 records API 共 `5 files / 93 tests` 全绿；其中票 05 的保存等待、旧响应晚到、取消与超时回归保持通过。`scripts/build-all.ps1` 前端类型检查/构建与网关编译通过。
- 失败 trace 与诊断证据保存在任务产物目录 `C:\Users\Administrator\.codex\visualizations\2026\11\20\01a1f7ad-e80f-71d0-bf64-ba6b18f09738\rf06-artifacts`，不进入仓库提交。
- H-46/H-47/H-48 请求与响应契约未改；现有共享 8080/8090 进程未被替换，所有 live 验收使用独立 8091 网关。远端 `origin` 当前网络不可达且无该集成分支；本地 integration 仍为 `4bf9e03`，无更新可合并。

