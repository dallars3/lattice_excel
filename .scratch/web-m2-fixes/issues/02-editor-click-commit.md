# 02: 下拉编辑器点击选中与提交链修复（BUG-01+BUG-04 网格部分）

**What to build:** 单选下拉点击选项即提交并收起编辑器；多选点击为勾选切换且编辑器保持打开；回车仅在搜索词为空时提交已选集合（有搜索词先匹配/勾选不提交旧集合）；方向键在选项间移动高亮并回车选中；选项按钮按下不夺走输入框焦点、点击不冒泡到单元格；失焦提交一律取编辑器最新草稿。单选/多选/人员/关联四类下拉编辑器统一落地。

**Blocked by:** 01（弹层 portal 化后点击交互才可演示）。

**Status:** done

- [x] 网格内单选：点击选项后单元格立即显示该选项标签，编辑器收起
- [x] 网格内多选：连续勾选 ≥2 个选项后失焦/回车，单元格显示全部已选标签
- [x] 多选搜索框输入选项名+回车：该选项被勾选且编辑器保持打开，不提交旧集合
- [x] 方向键移动高亮（aria-activedescendant）、回车选中高亮项；Esc 取消不落盘
- [x] 三键语义（Enter 提交下移/Tab 右移/Shift+Tab 左移）与校验失败不落盘行为不变
- [x] 挂载级 RTL：真实 focus/blur 链路下点击选项提交成功（补齐现有测试只走键盘路径的缺口）
- [x] 既有编辑器与网格写链用例全部保持绿

## Comments

**实现记录（2026-11-18）**
- 分支 `fix/wm2f-02-editor-click-commit`，提交 `9b47ffe`（组件级提交链）+ `bdf6cd3`（挂载级草稿新鲜度）+ `f80a56b`（并入集成 tip），合并入集成 `fix/web-m2-grid-fixes`（合并提交 `2ecd5fa`）
- 全量 `npm test`：86 文件 / **1093 用例全绿**（新增 14 条：组件级 9 + 挂载级 5，TDD 红→绿，挂载级「多选连勾」修复前 RED 实证复现）；`tsc --noEmit` 干净
- 要点：四类下拉统一点击语义（单选点击即提交收起；多选 toggle 保持打开）；修多选回车顺序缺陷（搜索词非空时零提交、只勾选）；新建选项草稿改为勾选后完整集合；方向键 aria-activedescendant 高亮+回车选中；选项/移除按钮 pointerdown preventDefault + stopPropagation；commitPendingDraft 改读 editorDraftRef（onDraftChange 同步直写），消除批处理下读到旧闭包草稿的时序缺陷
- **新发现根因链（超出探索笔记）**：portal 化后弹层点击仍经 React 组件树冒泡到单元格 handleCellClick → focusViewport 真实抢焦 → 失焦提交吞掉勾选；修复双保险 = 编辑器控件 stopPropagation + handleCellClick 对 EDITOR_POPUP_ATTR 弹层来源点击直接返回
- 备注：ref 化草稿是票面强制的时序加固（jsdom 下 ref 与已 flush state 等价，真实浏览器同批次竞态由它兜底）
