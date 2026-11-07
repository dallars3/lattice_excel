# 12: WS 事件流

**What to build:** 字段/表/视图结构写成功后向房间广播 `structure_changed`（含 seq/table/field）；断线重连通过 `join.last_seq` 或 `replay_request` 补齐缺失增量，seq 单调、不重不漏；通知帧不进入快照与重放。对应缺陷 #18（结构广播与 replay 部分）。

**Blocked by:** 11（共享连接生命周期与房间语义）

**Status:** done

- [x] 新增字段后同房间其他连接收到 `structure_changed`，字段信息完整。
- [x] 断线期间产生 3 次变更，重连补齐全部增量且无重复 seq。
- [x] 快照 seq 与后续增量衔接，不出现缺口。
- [x] `notification` 帧不进入快照/重放。

## Comments

已收口。16 条结构写路径同事务发布 record.changed(structure) + realtime RPCEventStore 断线重放（seq 去重、单调、join.last_seq 等价、通知帧不入快照/重放）；TC-090-05/07 绿。
