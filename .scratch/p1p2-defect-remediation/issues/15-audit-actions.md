# 15: 审计动作补齐

**What to build:** 表删除与数据导出成功后进入企业审计；导出记录携带 `client_request_id`；审计 CSV 可导出；保留期与幂等语义不变。对应缺陷 #23。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] 删除数据表后审计出现 `TABLE_DELETED`。
- [x] 导出成功后审计出现 `DATA_EXPORT`，`detail_map` 含 `client_request_id`。
- [x] 审计 CSV 包含上述动作。
- [x] 同一事件重复消费不重复落库。

## Comments

已收口。table.deleted 同事务出箱（EventOutbox seam）+ export.audited 改 RegisterExternal（此前被 outbox 存在性检查丢弃——DATA_EXPORT 缺失的真正根因）+ client_request_id 入 detail_map；TC-150-04/05 绿。
