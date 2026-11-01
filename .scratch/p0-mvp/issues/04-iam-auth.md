# 04: iam 注册·登录·会话

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 01 bootstrap

**Status:** done

- [x] 注册路径①（待邀）②（link_token）③（建企业）结果正确，HTTP 与 gRPC 双层断言通过
- [x] 同一「用户名+IP」连续 5 次失败锁定 15 分钟；成功登录签发 30 天票据
- [x] ExchangeSession 返回携带 enterprise_id/internal/link_context 的 Principal
- [x] 用户名唯一（忽略大小写）、密码规则、凭旧密码改密的错误码断言通过
