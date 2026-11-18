# 05: 表头行头占位对齐（BUG-03）

**What to build:** 表头行首渲染与数据行同宽（行头 64px）的占位单元：记录可勾选时为全选复选框（勾选/清空当前视图记录选择），否则空占位；表头与数据行两侧的冻结列 sticky 偏移统一计入行头宽度；横向滚动时占位与行头保持对齐。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 打开任意表：数据行每个单元格与其字段表头逐列几何对齐（挂载级 RTL 断言列矩形偏移为 0）
- [x] 全选复选框勾选后所有可见记录进入既有记录选择状态，再点取消全选
- [x] 冻结首列横向滚动时与表头保持对齐
- [x] 无记录选择权限的角色看到空占位而非复选框
- [x] 行高/分组行/虚拟滚动场景下对齐不回归（既有网格窗口用例保持绿）

## Comments

**实现记录（2026-11-18）**
- 分支 `fix/wm2f-05-05-header-row-alignment`，实现提交 `093a11e`，并入集成分支 `fix/web-m2-grid-fixes`（与票 04 的 GridArea.tsx 改动自动合并成功）
- 全量 `npm test`：84 文件 / 1045 用例通过；`tsc --noEmit` 通过
- 要点：FieldHeaderRow 新增 rowHeaderWidth/selectable/allSelected/onToggleSelectAll，行首 64px 占位（可勾选时含全选复选框）；frozenOffsets 从行头宽起算；数据行冻结格 sticky left 由内联 frozenOffset 下发（64px + 前序冻结列宽累加）；新增 GridArea.records.test.tsx 5 用例（TDD 红灯精确复现 BUG-03 的 [64,64,64] 偏移后转绿）
- 未触碰写提交链与刷新逻辑（票 06 范围），规避并发合并冲突
