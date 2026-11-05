# 10: share-link-entry（N6 分享链接入口）

**What to build:** 网关新增 `GET /s/{link_token}` 路由：调既有链接解析取 Base；短期缓存链接上下文（TTL<60s）；302 重定向——已登录至 Base 页、未登录至登录/注册页并携带 token；复用既有注册携带 token 的通道。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 已登录访客 302 至 Base 页；未登录访客 302 至登录/注册页且携带 token
- [ ] 链接上下文写入短期缓存并可被登录/注册流程消费
- [ ] 未知或失效 token 行为明确（错误页或引导页）
- [ ] 与静态托管路由共存不冲突
- [ ] 表驱动路由测试全绿；文档护栏脚本全绿（RPC 计数保持 111）

## Answer
已落地（N6 分享入口）。`GET /s/{link_token}`（gateway/share_link.go）：未登录→302 `/register?link_token=…`（复用注册携带 token 通道）；已登录→短期缓存（Redis，TTL 55s，故障放行）→ExchangeSession 取 principal.LinkContext 校验 token→302 `/bases/{base_id}`；无法解析→302 `/err?kind=share-link-unavailable`。零新增 rpc（RPC=111 不变）。**裁定备注**：spec 所述「既有链接解析 rpc」在 proto 全部 111 个 rpc 中不存在；采用既有注册携带 token 通道（core iam/register.go → sessions 持久化 LinkContext → ExchangeSession 取回）闭环，接口文档 v0.7 已按此书写。Evidence: merge `391bf0a`。