# H-71 企业外链清单缺 base_name / manager

Status: needs-triage
Severity: P1
Source: TC-130-06（API）

## 复现步骤

1. Base 开启「任何人」链接（role=filler）。
2. 企业管理员 `GET /api/v1/enterprise/open-links?limit=100`。

## 实际

```json
{"base_id":"...","base_name":"","manager":"","role":"ROLE_FILLER","opened_at":"..."}
```

`base_name` 与 `manager` 恒为空字符串。

## 期望

按 H-71：`link_list arr[{base_id, base_name, manager, role, opened_at}]`；管理员据此识别外链归属与责任人。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-130-06`

## 影响

企业管理器无法识别是哪张表、由谁开启的外链，强制关闭操作缺少上下文。
