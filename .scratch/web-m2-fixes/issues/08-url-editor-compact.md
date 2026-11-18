# 08: URL 编辑器紧凑化（BUG-02）

**What to build:** 网格单元格内的 URL 编辑器只渲染地址输入框（显示文本缺省取地址本身）；记录详情抽屉保留地址+显示文本双字段完整编辑；http/https 地址校验与既有三键语义不变。

**Blocked by:** 02（与提交链修复同批编辑器文件，串行避免冲突）。

**Status:** done

- [x] 网格内 URL 编辑只见一个地址输入框，输入合法地址提交后单元格渲染为链接
- [x] 抽屉内可分别设置地址与显示文本，单元格按显示文本渲染
- [x] 非法地址行内报错且不提交；清空提交 null
- [x] 显示文本缺省规则（未单独设置时取地址）在网格与抽屉一致
- [x] 组件级 RTL 覆盖紧凑/完整两种形态

## Comments

**实现记录（2026-11-18）**
- 分支 `fix/wm2f-08-url-editor-compact`，实现提交 `22ee5d6`，并入集成分支 `fix/web-m2-grid-fixes`（合并提交 `893b06e`，收尾已并票 07 tip 干净合并）
- 全量 `npm test`：合并票 07 后复跑 89 文件 / **1104 用例全绿**；`tsc --noEmit` 通过
- 要点：UrlCellEditor 新增宿主可控紧凑标志 config.compact（注入模式与 createOption/users 一致）；网格 GridArea 对 url 字段注入 compact:true（仅 +5 行）→ 单地址输入、display_text 缺省取地址；抽屉不注入即保留双字段完整编辑（RecordDrawer 零改动，完全避让票 03 宿主提交缝）
- 附带修正（提交值契约一致性）：草稿通道 onDraftChange 此前发驼峰 {url, displayText}，失焦提交缝直发 API 会落盘驼峰导致显示文本丢失；两通道统一为 {url, display_text}
- 测试：组件级 5 例（双形态/缺省/清空 null/非法报错）+ 挂载级 1 例（单输入框 + PUT body 契约 + 渲染链接）
