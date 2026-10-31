# 18: 契约测试补全与端到端回归

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 15 share-governance；17 admin-ops-enterprise-lifecycle

**Status:** ready-for-agent

- [ ] 三层测试缝（HTTP 93 + gRPC 110 + WS 16）一键运行并输出通过率
- [ ] 通用契约检查项 C-01~C-16 全覆盖
- [ ] 失败定位包含接口与错误码断言，回归入口有说明
- [ ] 与接口文档清单数量核对一致
