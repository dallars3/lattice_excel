# 03: design token 与 ui-kit 最小集（RTL 配对）

**What to build:** 全站样式基线：design token 以 CSS 变量落地（色板/间距/字号/三档行高/圆角/z-index/presence 专属色），亮色为默认、暗色仅留 `[data-theme="dark"]` 扩展位不实现；ui-kit 基础件最小集（Button/Banner/Toast/EmptyState）可被页面与后续网格演示直接使用，且各自有组件测试证明外部行为。

**Blocked by:** 01: 工程底座

**Status:** ready-for-agent

- [x] token 变量清单可消费，抽样组件源码不出现硬编码颜色/间距魔法值
- [x] Button 支持主要/次要/危险三类样式与禁用态
- [x] Banner 支持信息/警告两种语义；Toast 支持入栈、自动消失、手动关闭；EmptyState 支持插图 + 文案 + 操作按钮
- [x] 上述每个基础件至少一条 RTL 测试绿（断言外部行为：可见文案、禁用不可点、消失时机）
- [x] 暗色覆盖层占位存在且默认不生效（亮色渲染不受影响）

## Comments

- 实现：tokens.css 补全语义 token 全清单（中性/品牌/状态四色/功能色、presence 10 色、间距、字号、三档行高、圆角、z-index 分级）；暗色空覆盖位独立成 `styles/dark.css`（main.tsx 引入，默认无元素设置 data-theme，亮色不受影响；单独文件避免 @import 内联空规则构建告警）。ui-kit 四件：Button（三 variant + 原生 disabled + className/原生属性透传 + forwardRef）、Banner（info/warning）、Toast（provider + useToast，入栈/自动消失/手动关闭，时长可配 DEFAULT_TOAST_DURATION_MS）、EmptyState（内联 SVG 插图 + 文案 + 操作按钮）。
- 测试：tokens.test.ts（清单完整性 + 暗色只在 dark.css）、token-usage.test.ts（组件源码零魔法值抽查）、四组件各 ≥1 条 RTL 行为用例（Toast 消失时机用 fake timers）。全量 `npm run test` **102/102 绿**；`npm run build` 成功（34 modules，首屏资源 3 个）。
- 过程备注：implementer 代理被并发会话切换分支干扰中断（其 dev server 残留进程已清理），orchestrator 接管收尾——修正 tokens.css 注释措辞（原文含 `[data-theme=...]` 字面量触发暗色隔离用例误报）、删除临时诊断配置 vite.config.diag.ts、复跑全量验证。
- Commit：feat 见 feature/m1-web @ 1d9c197（曾误提交到 backend-align 分支为 5a45cdd，cherry-pick 归位；杂散提交留在对方分支，内容一致）。