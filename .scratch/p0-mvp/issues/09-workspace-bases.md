# 09: workspace Base 资产与首页

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 08 structure

**Status:** ready-for-agent

- [ ] 新建 Base 自动包含默认 1 表 3 字段（经模块接口编排一次成功）
- [ ] 首页四分组（最近访问≤50/我创建/与我共享/星标）与邀请待办聚合正确，局部失败返回 partial 而不 500
- [ ] 复制 Base 得到结构与数据但评论/历史为空
- [ ] 删除 Base 级联清空各模块数据并进入审计
