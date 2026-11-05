# 03: tracking-sink（埋点落库与保留）

**What to build:** core 服务可靠订阅 `track.reported`（消费组 + ack），批量落库；新增埋点表与操作审计分离（迁移 up/down 成对、表名全局唯一、event_id 唯一约束兜底幂等）；保留期自动清理任务；消费者停机窗口内事件不丢（可靠投递，重连后补投）。

**Blocked by:** 02 (tracking-ingest)

**Status:** resolved

- [ ] 迁移成对且表结构含 event_id 唯一约束，护栏迁移自检通过
- [ ] 消费者批插后 ack；重复 event_id 落库不新增行
- [ ] 保留期任务清理过期事件
- [ ] 消费者测试挂载沿用既有审计/导入消费者的内存事件总线 harness
- [ ] 文档护栏脚本全绿（proto RPC 计数保持 111）

## Answer
已落地。core 新消费者（消费组 core-tracking）订阅 `track.reported`，与网关 payload 逐字段对齐，批插后 ack、event_id 唯一约束兜底幂等；迁移 `000017_tracking.{up,down}.sql`（表 `excel_tracking_events`，与操作审计表分离）；worker `retention_cleanup` 接线清理（180 天，按 received_at_ms）。测试：内存总线 harness、worker 保留期测试；护栏迁移自检通过。Evidence: merge `9f89cc7`。