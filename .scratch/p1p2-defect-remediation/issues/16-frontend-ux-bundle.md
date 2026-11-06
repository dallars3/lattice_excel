# 16: 前端体验收口（修改人 / 搜索 / 压缩包提示）

**What to build:** 首页 Base 卡片显示可读的最近修改人；网格支持 `Ctrl+F` 在当前已加载数据内搜索并高亮；压缩包附件上传成功后提示「不解压查杀，请确认来源可信」。对应缺陷 #28、#29、#31。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] H-21 Base 摘要的修改人为显示名（缺失回退用户名），卡片不再出现内部 user_id。
- [ ] `Ctrl+F` 打开视图内搜索，过滤并高亮已加载数据，Esc 关闭，不修改服务端视图配置。
- [ ] `.zip` 上传成功并展示风险提示；`.exe` 仍被拦截。
- [ ] 上述行为均有 Playwright 用例覆盖。
