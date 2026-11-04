# 07: grid 虚拟滚动核心（窗口计算 + 选择模型 + 键位状态机）

**What to build:** 网格的 DOM 无关纯逻辑核心：虚拟滚动窗口计算（三档行高估算 + 实测校准 + overscan）、冻结列双通道列划分、已加载窗口区间 LRU 管理、SelectionModel（单元格/区域/行）、键位状态机。全部函数/状态机可单测，是 08 注册表与 09 演示页的地基。

**Blocked by:** 01: 工程底座

**Status:** ready-for-human

- [x] 窗口计算表驱动测试覆盖顶部/中部/底部/分组行混合：给定滚动偏移与行高表，输出可见行集合正确
- [x] 行高校准：估算值 → 实测值后，可见集随校准收敛（不跳行不重复）
- [x] 冻结双通道：给定冻结列数与总列数，固定区/滚动区列集合划分正确
- [x] 已加载窗口区间（loadedRanges）超上限按 LRU 淘汰（可测）
- [x] 键位状态机主路径表驱动绿：导航/选中/编辑/提交/取消、Tab/Shift+Tab 跨列、末行方向下键触发新增信号

## Comments

- feat commit：`7e9bbd1`（分支 feature/m1-web）。
- 落点：`web/src/core/grid/{common,windowing,row-height,frozen-columns,loaded-ranges,selection,keyboard}.ts`（一模块一文件，公共类型收 common.ts）+ 同名 `*.test.ts` 表驱动配对 + `index.ts` 门面转正（票 01 占位注释转真实出口）。全部 DOM 无关纯函数/纯状态机，未装任何新依赖（package.json/lockfile 零改动）。
- 各模块语义与验证：
  - **窗口计算 windowing**：`computeWindow(尺寸表, 滚动偏移, 视口尺寸, overscan?)` 轴无关（纵/横共用同一引擎，技术方案 6.1「横向滚动复用同一虚拟化引擎」）——前缀和 + 二分定位，输出可见节集合（index/offset/size，索引严格连续即不跳行不重复的结构保证）+ contentStart/contentEnd（不含 overscan）。表驱动 15 用例覆盖顶部（offset 0）/中部/底部（末行贴底）/分组行混合（分组行高 80 与普通行 40 交错）/单行高于视口/越界与负偏移钳制/overscan=0/零视口，期望值全手算字面量；另有无变量不变量扫描（随机偏移 × 混合行高表）。`FRAME_BUDGET_MS=16` 常量已落（消费策略注释锚定：超出降级=暂停 overscan 扩充，M1 不做降级实现）。
  - **行高校准 row-height**：三档估算 `ROW_HEIGHT_TIERS`（compact/medium/high，估算 32/40/48px——事实源未给像素值，取行业常规默认，构造可覆写并注明）+ `RowHeightModel`：`setMeasured` 实测回写缓存（Map<rowIndex, measuredHeight>，非法值显式拒绝）、`heightAt` 实测覆盖估算、`sizes` 直接可作 computeWindow 输入；分组行经注入 `estimateFor` 按行估算，实测同样覆盖。收敛测试构造「估算 32 偏小 → 实测 64 偏大」：同一偏移下迭代「切窗→实测→回写→再切窗」，断言 contentStart 单调不增、不动点稳定，完全校准后窗口与真值表（字面量 [64]*100）切窗逐项相等。
  - **冻结双通道 frozen-columns**：`computeColumnLayout(列宽表, frozenCount, scrollLeft, viewportWidth)` → 固定区列集合（sticky 通道完整输出）+ 滚动区横向虚拟化窗口（复用 computeWindow，视口=viewportWidth−frozenWidth，全表列号=frozenCount+局部索引）；边界：frozenCount 0/越总列数/负值钳制/视口被固定区占满/空表。
  - **loadedRanges LRU**：`LoadedRangeCache`（Map<cursor, Page> 语义）——put（insert 或更新）与 get 触及刷新新鲜度，has 只查询不触及；超上限（`DEFAULT_LOADED_RANGES_CAPACITY=10`，构造可配，<=0 显式拒绝）淘汰最久未触及页，被淘汰页完整记入 `evicted` 观测日志（内容不静默丢弃）；`clear` 对齐技术方案 6.1「筛选/排序/分组变更时清空」且不计入 LRU 日志。表驱动 6 组操作序列（get 触及免淘/put 更新免淘/has 不触及/容量 1 连续淘汰/未超上限不淘汰）+ 默认 10 页边界。
  - **SelectionModel selection**：纯函数三态（cell/range/row + all）；区域用 anchor+focus 拖选语义（`beginRange`/`extendRange` 锚点不动），`selectionRect` 归一化左上/右下界（反向拖选），越界一律钳制、空表选择为 null；全选边界（空表 null/单格表 kind=all）；`containsCell`/`cellCount`。
  - **键位状态机 keyboard**：`applyKey(state, keyEvent, bounds) → {state, effects}` 纯函数，键清单按需求 3.8.1 全落（方向键/Enter/Tab/Shift+Tab/F2/Esc/Delete/Ctrl+Z/C/V/X/F）；副作用信号枚举 beginEdit/commit/cancel/clearCells/clipboard(copy|cut|paste)/undo/find/appendRow（信号而已，M1 不实现新增/剪贴板/撤销/查找逻辑）。主路径：idle 态方向键移动+边界钳制、末行 ↓ 与末行 Enter 触发 appendRow（需求 3.8.1「末行数据区等价触发新增记录」+ 4.4「末行方向下键」）、F2/Enter 进编辑（3.8.2）、编辑态 Enter 提交下移/Tab 提交右移/Shift+Tab 提交左移/Esc 取消不落盘（AC-REC-02）、编辑态其余键归编辑器持有（no-op 且原状态引用返回）、Delete/Ctrl 组合信号携带 anchor..focus 归一化矩形、双击通道 `requestEdit` 与 F2 同一 beginEdit 迁移（3.8.2 三通道）、`withSelection` 为票 09 拖选接线缝。表驱动 30 用例（含 15 行键位主路径全表逐行断言）。
- 验证：`npm run test` 387/387 绿（基线 294 不回归，新增 93：windowing 15 + row-height 10 + frozen-columns 12 + loaded-ranges 13 + selection 13 + keyboard 30）；`npm run build` 成功（tsc --noEmit 无类型错误），JS chunk **4** ≤6（grid 核心纯逻辑不进首屏构建图，随票 09 演示页接线后按既有 manualChunks 归组）。TDD 红-绿逐片推进（六片均先确认红态再实现转绿；过程红含 3 处测试期望值手算错误，均按事实源改测试不改实现）。
- 偏差：
  - Tab/Shift+Tab 行尾回绕：需求文档键位表仅定义「提交并右移/左移」，未定义行尾回绕语义（票面要求「按需求文档」而文档无此定义）→ 取保守钳制不回绕（行尾提交仍发生、焦点钳在边界列），已在 keyboard.ts 注释注明依据。
  - 三档行高具体像素值：三份事实源均未给定数值（需求文档仅「紧凑/中等/高」）→ 落 32/40/48px 行业常规默认，`ROW_HEIGHT_ESTIMATE_PX` 常量可整档覆写，票 09 演示页接入 token 时可调整。
  - `stores/selection.ts`（票 02 骨架）的 CellRef 类型收敛推迟到票 09 接线时一并落（plan §6「后续票不回改共享文件」；本票 SelectionModel 为纯函数核，store 接线是票 09 的事）。
  - ticket 09 演示页将消费 `withSelection`/`requestEdit` 接缝与 `LoadedRangeCache`，AC-VIEW-07/AC-NFR-01/AC-NFR-03 的通过性验证按 spec 后置（M2 起有真实数据与页面）。