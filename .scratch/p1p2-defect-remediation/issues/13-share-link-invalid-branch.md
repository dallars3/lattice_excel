# 13: 分享链接失效分支

**What to build:** 伪造或已失效的分享 token 一律跳转失效页，不再引导注册；有效 token 保持三态：未登录进注册、已登录进 Base、重置/关闭后立即失效。对应缺陷 #20。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 伪造 token 302 到 `/err?kind=share-link-unavailable`，页面展示「分享链接不可用」。
- [ ] 有效 token 未登录 302 到注册并携带 `link_token`；已登录 302 到 `/base/{id}`。
- [ ] 重置/关闭链接后旧 token 立即失效（不等待缓存 TTL）。
- [ ] 有效链接注册后直接进入对应 Base。
