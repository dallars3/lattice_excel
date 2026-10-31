# 03: share 权限裁决骨架

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 04 iam-auth

**Status:** ready-for-agent

- [ ] permissions.json 启动加载，配置更新后新请求按新策略生效
- [ ] 内部成员/外部协作者/任何人链接三类身份的裁决结果正确（gRPC 契约断言）
- [ ] 外部请求 export 一律拒绝；外部显式角色上限 editor 生效
- [ ] 收窄或关闭链接发布 link.closed，1 分钟内鉴权缓存可见失效
