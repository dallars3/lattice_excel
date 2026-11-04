# 08: CellRenderer / CellEditor 注册表 + 演示渲染器

**What to build:** 按字段类型注册单元格渲染器与编辑器的框架（网格渲染流按注册表取实现，未知类型有兜底），并交付 text / number / select 三个演示实现：text 单行省略、number 按格式渲染、select 带颜色标签；编辑器统一生命周期（开始/校验/提交或拒绝），Enter/Tab/Esc 行为一致。为 M2 起 11 种字段类型的注册扩展定型接口。

**Blocked by:** 07: grid 虚拟滚动核心

**Status:** ready-for-agent

- [ ] 注册表可注册/查询/去重；查询未知类型返回兜底渲染器
- [ ] text 演示：长文本单行省略，全量值可获取
- [ ] number 演示：整数/小数/千分位按配置渲染
- [ ] select 演示：选项标签按色渲染
- [ ] 编辑器统一生命周期 begin → input → validate → commit/reject，三键（Enter 提交/Tab 提交右移/Esc 取消）行为一致
- [ ] 三个演示渲染器 RTL 冒烟绿（渲染不崩溃、外部语义可断言）