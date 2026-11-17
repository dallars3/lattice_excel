# 01: 下拉弹层 portal 化 + 视口翻转（BUG-06）

**What to build:** 单选/多选/人员/关联的选项面板、日期选择器的日历、编辑器行内错误提示，统一改为渲染到文档根节点（portal），以触发单元格为锚定位；弹层不再被网格单元格的 overflow 裁剪，靠近视口边缘时自动翻转方向。本票是后续点击交互工单的 prefactor。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 在网格任意行（含最后一行）打开日期编辑器，日历完整可见且可点选日期，点选后值写入单元格
- [x] 单选/多选/人员/关联的选项面板在任意行完整可见
- [x] 校验失败的行内错误提示不被裁剪
- [x] 挂载级 RTL 断言弹层节点不在滚动/裁剪祖先容器内
- [x] 弹层最小宽度与既有下拉层级语义保持（不被顶栏/既有弹窗压住）
- [x] 既有编辑器键盘路径用例全部保持绿

## Comments

**实现记录（2026-11-18）**
- 分支 `fix/wm2f-01-01-portal-popups`，提交 `c00594b`（portal 工具）+ `d248528`（弹层 portal 化）+ `6351e2a`（并入集成分支 tip），合并入集成 `fix/web-m2-grid-fixes`（合并提交 `af160ef`）
- 全量 `npm test`：合并 tip 后 **1077 通过 / 0 失败**；`tsc --noEmit` 通过
- 要点：新增共享工具 EditorPopup（body portal + position:fixed 锚定 + scroll/resize 重定位）与 resolvePopupPlacement 纯函数（下方优先、空间不足翻转上方、水平收拢 8px 边距）；四类下拉面板/日历/行内错误全部 portal 化；GridArea editorHost onBlur 归属判定扩展——焦点落在本编辑器 portal 弹层内视为未离开、不触发提交（语义不变）；附带消除了弹层点击冒泡进单元格的夺焦竞态
- **提交链（onCommit/onClick）一行未动**，属票 02 范围
- 遗留备忘（供票 02/收尾参考）：GridArea 写失败提示 cell-write-error 仍会被单元格裁剪，属写失败路径非编辑器校验错误，本票未动
