# 字段类型转换预览返回 9001

Status: needs-triage
Severity: P1
Source: TC-040-16（API 复核 + UI）

## 复现步骤

1. 创建文本字段并写入一条值。
2. `POST /api/v1/fields/{id}/convert`，body `{target_type:"number", confirm:false}`（预览）。

## 实际

`{"code":9001,"message":"服务暂不可用"}`；UI 转换对话框无法展示「受影响记录 N 条 / 将清空值 M 个」，确认转换不可达。

## 期望

按 H-38 / AC-FLD-06：预览返回 `{affected_count, cleared_count}`；确认后返回转换后的字段。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → 无直接用例（TC-040-16 为 UI）
- `.scratch/fullflow-test/probe4.mjs` 输出：`convert preview 9001`
- UI：`TC-040-16` 失败，对话框内无影响预览

## 影响

字段类型转换（P1 字段能力）整条不可用；删除字段的影响面预览同样受影响（见下一缺陷）。
