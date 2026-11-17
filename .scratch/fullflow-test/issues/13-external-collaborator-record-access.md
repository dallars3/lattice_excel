# 外部协作者读取/写入记录返回 4002/9001

Status: needs-triage
Severity: P1
Source: TC-050-14 / TC-100-05（API）

## 复现步骤

1. 外部账号（无企业归属，A8/peer）被添加为 Base 可编辑协作者。
2. 该账号调用：
   - `GET /api/v1/records/{record_id}?base_id=...`
   - `GET /api/v1/bases/{id}/records?table_id=...`
   - `PUT /api/v1/records/{record_id}/cells/{field_id}?base_id=...`

## 实际

- 读单条记录：`4002 记录/字段已被删除`
- 读记录列表：`4002 记录/字段已被删除`
- 写单元格：`9001 服务暂不可用`

## 期望

外部可编辑协作者在授权 Base 内应能正常读记录；写请求按乐观锁返回成功或 4001/3001，不应返回 4002/9001。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-050-14`、`TC-100-05`
- `.scratch/fullflow-test/probe2.mjs` 复现输出（同一 Base、同一记录，owner 正常，外部协作者 4002/9001）

## 影响

外部协作者（链接邀请/显式邀请）核心读写链路不可用，且 4002 会误导前端认为数据已删除。
