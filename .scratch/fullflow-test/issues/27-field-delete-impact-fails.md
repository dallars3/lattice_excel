# 删除字段影响面查询失败（1001/9001）

Status: needs-triage
Severity: P1
Source: TC-040-18（API 复核 + UI）

## 复现步骤

1. 创建任意字段。
2. `DELETE /api/v1/fields/{id}`，body `{confirm:false}`（查询影响面）。
3. 再以 `{confirm:true}` 删除。

## 实际

- `confirm:false` → `1001 参数缺失或非法`
- `confirm:true` → `9001 服务暂不可用`
- UI 删除字段对话框无法展示影响记录数/依赖公式数/依赖引用数；确认删除不可达。

## 期望

按 H-39 / AC-FLD-07：先返回 `{impact:{record_count, dependent_formulas, dependent_lookups}}`，确认后删除并清理视图条件。

## 证据

- `.scratch/fullflow-test/probe4.mjs` 输出：`delete impact 1001`、`cleanup 9001`
- UI：`TC-040-18` 失败（对话框无影响面）

## 影响

字段删除（含被视图条件/公式/引用依赖的字段）无法安全执行，用户可能被迫绕过确认或无法删除字段。
