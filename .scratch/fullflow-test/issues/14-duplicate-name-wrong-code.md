# 重名错误码不符合契约：表/字段返回 1001 而非 1002

Status: needs-triage
Severity: P1
Source: TC-160-04（API）

## 复现步骤

1. `POST /api/v1/bases/{id}/tables` 创建表 `重名表xxxx`（返回 0）。
2. 再次以同名创表。
3. 对已有字段名 `数量` 再次 `POST /api/v1/tables/{id}/fields`。

## 实际

两次重名均返回 `1001 参数缺失或非法`。

## 期望

按 H-30 / H-35 / 附录 B：对象重名返回 `1002`，前端据此定位输入框并提示「名称已存在，请更换」。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-160-04`
- `.scratch/fullflow-test/probe2.mjs`：`first 0 second 1001`

## 影响

前端无法区分「参数非法」与「重名」，错误提示与字段锚定全部失真。
