# H-35 字段描述未接线（已知缺口，P2 用例失败）

Status: needs-triage
Severity: P2
Source: TC-040-20（API）

## 实际

`POST /api/v1/tables/{id}/fields` body 带 `description` 时返回 `1001 参数缺失或非法`（网关 `DisallowUnknownFields`，body 仅解码 name/type/config_map）。

## 期望

按 H-35 / N3：`description str≤500` 应写入并在字段列表返回。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-040-20`
- 前端已在 `web/src/features/workspace/api/field.ts` 注释中登记为「票 04 G 项：description 写入通道未接线」，当前仅会话内兜底。

## 影响

字段描述无法跨会话/跨协作者持久化；TC-040-20 无法通过。若确认 P0 不做，请在需求/接口文档中显式标注该能力后置，并将用例降级。
