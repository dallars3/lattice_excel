# 03: P0 网格首窗口就绪与新增记录精确基线

**What to build:** 网格只有在首窗口 Record 已加载并提交到 UI 后才报告就绪。P0 新增记录用例等待该就绪状态，以 H-48 的首窗口服务端基线建立 `before` 数量，并对三个新增入口同时验证服务端数量与 UI 最终数量。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 网格提供可访问且测试可读的首窗口 Record 就绪状态；仅出现网格容器或列头不得视为 Record 已就绪
- [x] P0 通用进入 Base 的等待逻辑同时要求网格、列头和首窗 Record 就绪
- [x] TC-050-01 操作前以 H-48 首窗口结果读取服务端基线，并等待 UI 数量与服务端基线完全一致后再记录 `before`
- [x] “页尾新增”“首行新增”“末行方向下键自动新增”三个入口分别验证服务端数量达到预期增量，并等待 UI 最终数量一致
- [x] 计数等待不使用固定 sleep；失败信息能区分“首窗口未就绪”和“服务端/UI 数量不一致”
- [x] 受控慢 H-48 场景证明修复前会在临时数量上过早开始，修复后不会
- [x] TC-050-01 连续重复执行 20 次零失败，既有游标窗口、虚拟滚动与 P0 其他用例不回归

## Comments

### 2026-11-20 — P0 网格首窗口就绪与精确基线收口

实现：

- `GridArea` 在首窗口 Record 已提交后暴露 `data-record-window-state="ready"` 与 `aria-busy="false"`；加载期间分别为 `loading` 与 `true`。
- `WorkspaceFooter` 暴露与可见文案同源的 `data-count` / `data-has-more`，作为 UI 数量断言接缝。
- P0 `waitWorkspace` 同时等待网格、列头、首窗口 Record 就绪状态和 `aria-busy=false`；受控慢 H-48 用例证明了旧观察条件会在响应释放前提前完成。
- TC-050-01 为每次执行创建独立 Base，先用 H-48 首窗口结果建立服务端基线并等待 UI 完全一致，再验证页尾新增、首行新增和末行方向下键自动新增三条路径的服务端/UI 双增量。
- TC-050-01 默认仍注册一个用例；设置 `P0_TC05001_REPEAT=20` 时在单次 spec 加载中注册 20 个唯一命名用例，每个用例执行完整基线及三条新增路径，避免 `--repeat-each` 重复触发 `beforeAll` 注册限速。
- 同步修正集成提交 `4f9c2d1` 中 agent 临时文件跳过规则的多余反斜杠，使受保护的 `.codex-agent-*` 文件无需修改即可通过文档护栏。

红/绿证据（此前实施已记录，本次按继续要求复用，未重跑单个红/绿切片）：

- 组件：`npm test -- src/features/workspace/components/GridArea.records.test.tsx -t "首窗口 Record 就绪状态"`。修复前因缺少 `data-record-window-state` / `aria-busy` 失败；修复后通过。
- 受控慢 H-48 Playwright：`$env:SF_BASE_URL='http://127.0.0.1:8090'; node node_modules/playwright/cli.js test p0.spec.ts --workers=1 --grep 'TC-050-01-SLOW'`。旧 `waitWorkspace` 在受控响应释放前提前 settled，用例红；改为等待 Record 窗口就绪后单个用例绿。
- 默认单次 TC-050-01 Playwright 用例在此前执行中通过。

本次补充验收：

- 20 次：`$env:SF_BASE_URL='http://127.0.0.1:8090'; $env:P0_TC05001_REPEAT='20'; node node_modules/playwright/cli.js test p0.spec.ts --workers=1 --grep '^.*TC-050-01 \|'`，结果 `20 passed / 0 failed`（31.6s）。
- 全量 P0：清空 `P0_TC05001_REPEAT` 后执行 `node node_modules/playwright/cli.js test p0.spec.ts --workers=1`，结果 `32 passed / 0 failed`（53.3s），覆盖游标窗口、虚拟滚动、受控慢 H-48 与既有 P0 用例。
- 环境提示：20 次运行后立即启动的首轮全量 P0 因共享注册窗口限速导致 TC-010-01 以及依赖它的 TC-020-01 失败；注册窗口恢复后无其他前置夹具干扰，重跑取得上述 `32 passed / 0 failed`，TC-050-01 与慢 H-48 两轮均始终通过。
- 组件：`npm test -- src/features/workspace/components/GridArea.records.test.tsx src/features/workspace/components/WorkspaceFooter.test.tsx`，结果 `30 passed / 0 failed`。
- 构建/类型检查：`npm run build`，`tsc --noEmit` 与 Vite production build 均通过。
- 文档护栏：`powershell.exe -NoProfile -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1`，结果 `fail=0`（检查 209 个 Markdown 文件）。

提交：

- 集成基线：`4f9c2d1`（`feature/race-closeout-followups`）。
- 实现与测试：`4bcf9378bf41dc14d9ac11ebaab5e747ebdea8cc`。

