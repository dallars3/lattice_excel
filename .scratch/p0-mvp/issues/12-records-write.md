# 12: records 写链

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 02 eventbus-outbox；10 records-read；11 calc-engine

**Status:** done

- [x] 版本不符返回 4001 并附对方新值；冲突三选不静默覆盖
- [x] 批量/粘贴结果、失败行与跳过计数正确；client_request_id 幂等
- [x] 删除 7 秒内可撤销，超窗返回 4002；回滚生成新历史节点
- [x] 同行公式同事务物化；record.changed/record.deleted 事件可观察
