# H-30 新建表不返回默认字段与视图（前端已兜底）

Status: needs-triage
Severity: P1（UI 用例通过，契约缺口）
Source: TC-030-01（API 侧复核）

## 复现步骤

1. `POST /api/v1/bases/{base_id}/tables`，body `{"name":"表二"}`。
2. `GET /api/v1/tables/{new_table}/fields` 与 `/views`。

## 实际

- `field_list=[]`、`view_list=[]`；表计数 `field_count=0, view_count=0`。

## 期望

按 AC-TBL-01 / TC-030-01：新表自带默认表格视图与主字段。

## 证据

- API 执行：`fields=0, views=0`
- 结果文件：`.scratch/fullflow-test/p0-api-results.json` → `TC-030-01`
- UI 用例通过原因：`web/src/features/workspace/BaseWorkspacePage.tsx::bootstrapTableDefaults` 在创建后补建「标题」字段与「表格」视图。

## 影响

当前只有本前端会补默认结构；H-30 的直接调用方、导入/复制等其它客户端可能得到空表。建议后端在 CreateTable 内原子创建默认结构与表记录（或明确契约要求由调用方创建）。
