# 10: records 读链

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 03 share-authz；05 gateway-authz-pipeline；08 structure

**Status:** ready-for-agent

- [ ] QueryView 按视图配置执行筛选（≤5 AND）/排序（≤3、空值末位）/分组与游标分页
- [ ] 可填写仅可见本人记录；关联候选与 Lookup 过滤不可见目标
- [ ] 操作历史 90 天、字段前后值与角色可见性正确
- [ ] HTTP 与 gRPC 双层契约断言通过
