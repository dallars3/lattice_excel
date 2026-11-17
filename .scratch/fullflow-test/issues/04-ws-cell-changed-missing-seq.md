# WS cell_changed 广播缺失 seq（恒为 0 被省略）

Status: needs-triage
Severity: P0
Source: TC-090-02（WS 协议级）

## 复现步骤

1. owner 与 peer 各建一条 WS 连接并 join 同一 base/table。
2. owner 通过 HTTP 更新某记录单元格。
3. 观察 peer 收到的 `cell_changed` 帧。

## 实际

- 帧在 1s 内到达，值与 version 正确，但载荷无 `seq` 字段：
  `{"record_id":"...","field_id":"...","value":{"int_value":"321"},"version":"5","operator":"..."}`

## 期望

按 W-09 / TC-090-02：帧携带 `seq` 且随事件单调递增（可支撑 W-07 replay 对账）。

## 证据

- 两次执行结果一致：`.scratch/fullflow-test/p0-api-results.json` → `TC-090-02`
- 根因线索：`excel_core_service/internal/records/write_options.go::recordChangedEvent` 未设置 `Header.Seq`；`excel_web_api/internal/realtime/subscribers.go` 直接将 `event.Header.Seq` 填入帧。
- `snapshot.seq` 同样恒缺省（protojson 省略 0）。

## 影响

P0：客户端无法按 seq 判定乱序/断线补齐；TC-090-01/02 的「seq 初始化一致、单调递增」不可满足，W-07 replay 语义失真。
