# 03: design token 与 ui-kit 最小集（RTL 配对）

**What to build:** 全站样式基线：design token 以 CSS 变量落地（色板/间距/字号/三档行高/圆角/z-index/presence 专属色），亮色为默认、暗色仅留 `[data-theme="dark"]` 扩展位不实现；ui-kit 基础件最小集（Button/Banner/Toast/EmptyState）可被页面与后续网格演示直接使用，且各自有组件测试证明外部行为。

**Blocked by:** 01: 工程底座

**Status:** ready-for-agent

- [ ] token 变量清单可消费，抽样组件源码不出现硬编码颜色/间距魔法值
- [ ] Button 支持主要/次要/危险三类样式与禁用态
- [ ] Banner 支持信息/警告两种语义；Toast 支持入栈、自动消失、手动关闭；EmptyState 支持插图 + 文案 + 操作按钮
- [ ] 上述每个基础件至少一条 RTL 测试绿（断言外部行为：可见文案、禁用不可点、消失时机）
- [ ] 暗色覆盖层占位存在且默认不生效（亮色渲染不受影响）