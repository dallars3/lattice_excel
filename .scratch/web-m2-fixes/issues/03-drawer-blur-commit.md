# 03: 记录详情抽屉失焦提交桥（BUG-04 抽屉部分）

**What to build:** 记录详情抽屉的编辑宿主补失焦提交桥：切换到另一字段或关闭抽屉前，在途草稿按最新草稿提交；草稿未改动时零请求直接切换；与网格区同语义。抽屉内的多选/人员/关联编辑一并受益。

**Blocked by:** 02（共享编辑器提交链先修复，提交的值才正确）。

**Status:** done

- [x] 抽屉内多选勾选 2 个选项后直接点击另一字段：原字段值保存成功
- [x] 抽屉内编辑后关闭抽屉：草稿不丢失
- [x] 草稿未改动时切换字段不发请求
- [x] 抽屉内写失败/版本冲突沿用既有错误提示与裁决 UI，不静默丢弃草稿
- [x] 挂载级 RTL 覆盖切换字段提交与关闭抽屉提交两条路径

## Comments

**实现记录（2026-11-18）**
- 分支 `fix/wm2f-03-drawer-blur-commit`，实现提交 `8b12884`，并入集成分支 `fix/web-m2-grid-fixes`（合并提交 `2b650e7`，含票 07/08 tip 无冲突）
- 全量 `npm test`：合并集成 tip 后 89 文件 / **1112 用例全绿**（新增挂载级 RTL 8 例，TDD 红→绿 5 条桥必需路径先红）
- 要点：RecordDrawer 编辑宿主补 onBlur 提交桥（relatedTarget 宿主内或 EDITOR_POPUP_ATTR portal 弹层内不打断，与 GridArea 同判定）；drawerDraftRef 承载最新草稿（与网格 editorDraftRef 同口径）；submitInFlightRef 同字段单飞防双发；写成功 setEdit(null) 改会话守卫（提交在途时切字段不误关新编辑器）；关闭按钮走 requestClose 先提交再 onClose；上一条/下一条切换前先提交在途草稿
- 编排者备注：实现者上报的 tsc 遗留错误（FieldConfigPanel/FieldCreateDialog 测试 mock 返回 Promise<FieldRecord> 与组件 Promise<void> 不符）已由编排者在集成上直接修复（mock 补 .then(() => undefined)），属票 07 新增测试文件的类型收口
