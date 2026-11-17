# 审计缺 TABLE_DELETED / DATA_EXPORT 动作

Status: needs-triage
Severity: P1
Source: TC-150-04 / TC-150-05（API）

## 复现步骤

1. 删除一张数据表；执行一次导出（H-85）。
2. `GET /api/v1/enterprise/audits?limit=100`。

## 实际

审计动作集合为 `["member_enabled","member_disabled","link_force_closed","record_deleted"]`，不含表删除与数据导出。

## 期望

按 H-87 / AC-IMPEXP-06 / 审计清单：`TABLE_DELETED`、`DATA_EXPORT`（含 client_request_id）应入审计。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-150-04`、`TC-150-05`

## 影响

表删除与数据外流两个高风险动作无审计留痕，合规与追责链路缺失。
