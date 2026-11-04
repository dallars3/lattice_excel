# 08: CellRenderer / CellEditor 注册表 + 演示渲染器

**What to build:** 按字段类型注册单元格渲染器与编辑器的框架（网格渲染流按注册表取实现，未知类型有兜底），并交付 text / number / select 三个演示实现：text 单行省略、number 按格式渲染、select 带颜色标签；编辑器统一生命周期（开始/校验/提交或拒绝），Enter/Tab/Esc 行为一致。为 M2 起 11 种字段类型的注册扩展定型接口。

**Blocked by:** 07: grid 虚拟滚动核心

**Status:** ready-for-human

- [x] 注册表可注册/查询/去重；查询未知类型返回兜底渲染器
- [x] text 演示：长文本单行省略，全量值可获取
- [x] number 演示：整数/小数/千分位按配置渲染
- [x] select 演示：选项标签按色渲染
- [x] 编辑器统一生命周期 begin → input → validate → commit/reject，三键（Enter 提交/Tab 提交右移/Esc 取消）行为一致
- [x] 三个演示渲染器 RTL 冒烟绿（渲染不崩溃、外部语义可断言）

## Comments

- feat commit：`fa6f758`（分支 feature/m1-web）。
- 落点：`web/src/core/grid/registry.ts`（框架）+ `web/src/core/grid/renderers/`（fallback 兜底、format-number 纯函数、text/number/select 演示、index 注册缝）+ `grid/index.ts` 门面转正（票 07 占位注释转真实出口）。未装任何新依赖（package.json/lockfile 零改动）；样式全走 tokens.css 变量 + CSS Modules（BEM），组件源零硬编码颜色/间距（select 注入色经字段配置 inline style，非源码写死）。
- 实现要点：
  - **注册表框架 registry**：两注册表 `renderers[fieldType]` / `editors[fieldType]`（技术方案 6.4），register/query 往返；类型键为精确匹配字符串（不归一化、大小写敏感），M2 的 11 种类型键以接口文档字段类型枚举为准（需求 4.3 矩阵）。去重策略选「后注册覆盖（last-wins）」，register 返回被替换的旧定义（undefined = 首次注册）——理由：M2「默认实现 + 场景覆写」需要覆写路径，报错会阻断它；返回旧定义让装配层可对意外覆盖断言/告警（已注释在文件头）。未知类型的渲染器/编辑器查询一律命中内置兜底。框架逻辑（注册/查询/去重/兜底/键位解析/提交流）DOM 无关直接单测；`defineRenderer`/`defineEditor` 为唯一型变收窄点（注册表以 unknown 存储全部定义，类型安全在各字段实现内闭环）。
  - **兜底 fallback**：渲染器安全显示原始值文本——null/undefined → 空串、其余 String() 化，永不抛错、不丢 0/false 等合法假值，全量值经 title 可获取；编辑器为通用字符串编辑（统一生命周期 + 三键一致，值形态未知故不做类型校验）。工程注记：fallback.tsx 对 registry 仅类型导入（运行时零依赖）——registry 运行时反向导入兜底作默认值，实现中曾因运行时回环在 vite 模块初始化期炸「defineEditor is not a function」（vitest 单文件跑不出、全量跑才炸），已消除并在文件头注释锚定。
  - **text 演示**：CSS ellipsis 单行省略（clip 语义元数据 + 样式模块挂载双断言）+ 全量值 title/文本节点双通道（需求 4.3 多行文本行「单行省略，hover 看全文」的 M1 网格态，抽屉看全文属 M2）；编辑器 maxLength 校验（缺省 10,000 对齐字段矩阵「≤10,000 字符」），validate 失败行内 `role=alert` + `aria-invalid` 阻止提交、内容不丢失，修正后错误即时消除。
  - **number 演示**：`formatNumber` 纯函数表驱动（整数/小数 0-4 位钳制、千分位开关、超精度四舍五入、空值与 0 区分、非有限值安全空显、-0 舍入归一），组件右对齐 + tabular-nums；百分比/货币格式（需求 4.3 数字行其余配置）注释后置 M2。
  - **select 演示**：按选项 id 渲染标签，色值来自字段配置注入（调色板分配/可改），标签底样式走 token、注入色仅替换背景并配 `--color-text-on-brand`；未配色选项走 token 中性样式；值不在选项列表时安全显示原始值不误配。
  - **编辑器统一生命周期**：`CellEditorLifecycle<Draft, Input, Config, Value>` 四钩子纯函数（begin/input/validate/commit），reject 为无钩子终止分支（取消编辑、丢弃草稿、不产出值，由会话持有方触发）；`resolveEditorKey` 三键解析全部编辑器共用——Enter 提交（move 默认 down、`enterMove` 可配对接票 07 键位信号 commit.nextFocus）、Tab 提交右移、Shift+Tab 提交左移、Esc reject，其余键 pass 交还原生行为；`commitEditorDraft` 统一提交流——validate 失败阻止提交（返回可访问错误文案，commit 钩子不执行）。text 编辑器为演示实现，M2 各字段类型按同契约落钩子即得一致三键行为。
- 验证：`npm run test` **470/470 绿**（基线 387 不回归，新增 83：registry 框架 22 + format-number 16 + number 8 + text 27 + select 7 + registerDemoCells 3）；`npx tsc --noEmit` 0 错；`npm run build` 成功，首屏 JS chunk **4** ≤6（index / vendor-react / vendor-router / vendor-query）。TDD 红-绿逐片推进（六片，均先确认红态再实现转绿）。
- 偏差：
  - 字段类型键 M1 用演示键 text/number/select；11 种类型的正式键取值需与接口文档字段类型枚举对齐，属 M2 注册时的事（框架键本就是字符串，无接口变更）。
  - 生命周期钩子的 config 容忍缺省（text validate 对 `config?.maxLength` 防御、TEXT_LIFECYCLE 的 Config 为 `TextCellConfig | undefined`）：注册表边界 config 为 unknown/可缺省，未配置字段的编辑仍须可用——由测试红态发现的真实缺陷修复。
  - 11 种字段类型完整渲染器/编辑器实现为 M2（spec Out of Scope），M1 仅框架 + 3 演示 + 兜底；演示编辑器为单行 input（多行文本域与换行处理随 M2 真实编辑器落）。
