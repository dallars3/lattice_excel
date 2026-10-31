# 08: structure 表/字段/视图

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 05 gateway-authz-pipeline

**Status:** ready-for-agent

- [ ] 11 种字段类型按 config 校验，非法配置返回 1001
- [ ] ConvertField 预览 affected/cleared 与确认结果一致；date→number 被拒绝
- [ ] DeleteField 影响面三计数（记录/依赖公式/依赖引用）与真实数据一致
- [ ] 视图配置 filters≤5、sorts≤3、group_by≤1 校验与即时保存；重名与容量上限错误码正确
