# WS 协议缺口：非法握手未拒绝、结构变更不广播、断线重放为空、presence 不下线

Status: needs-triage
Severity: P1
Source: TC-000-07 / TC-090-03 / TC-090-05 / TC-090-07（WS）

## 实际

| 用例 | 期望 | 实际 |
|---|---|---|
| TC-000-07 | 不存在的 base 握手拒绝 | 101 升级成功，连接打开 |
| TC-090-03 | A8 关闭连接后 A1 收到 `presence.online=false` | 6s 内无离场帧 |
| TC-090-05 | 新增字段后协作者收到 `structure_changed`（含 field_id） | 只收到 `snapshot`/`ack`，无结构帧 |
| TC-090-07 | 断线期间 3 次变更，重连 `last_seq`/`replay_request` 补齐且不重复 | 重放帧为空（`kinds=[]`） |

## 期望

按接口文档 §3 与 W-08/W-09/W-11/W-12/W-07：握手鉴权/存在性校验、结构变更广播、断线增量补齐、presence 上下线。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → 上述 4 条
- `.scratch/fullflow-test/p1p2-api-run.log`

## 影响

实时协作三要素（结构同步、在场状态、断线对账）均不可用；M3 实时 UI 无法按契约接线。
