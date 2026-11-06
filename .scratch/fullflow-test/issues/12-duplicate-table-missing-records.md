# 复制数据表不复制记录

Status: needs-triage
Severity: P1
Source: TC-030-04（API）

## 复现步骤

1. 在 table2 中创建 1 条记录与 1 条评论。
2. `POST /api/v1/tables/{table2_id}/duplicate`。

## 实际

- 接口返回 0，字段/视图复制成功；
- 副本 `records=0`（源表 1 条记录未复制）。

## 期望

按 TC-030-04：结构 + 数据 + 视图复制；评论不复制。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-030-04`：`fields=1, views=1, records=0`
- 代码：`structure.DuplicateTable` 通过 `service.impact.(TableRecordsDuplicator)` 的运行时类型断言触发记录复制；当前 impact 适配器未实现该接口，断言失败后静默跳过。

## 影响

复制表得到空表，用户会误以为数据丢失。
