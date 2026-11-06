# worker→core 调用缺 x-row-scope：导入全部行失败、导出恒 1001

Status: needs-triage
Severity: P0
Source: TC-140-01 / TC-140-02 / TC-140-09（API）

## 复现步骤

1. 以 owner 上传 CSV/XLSX（H-77 usage=import_temp）。
2. `POST /api/v1/imports/preview` → `POST /api/v1/imports`，轮询 H-83。
3. 或 `POST /api/v1/exports`，body 含 base_id/table_id/view_id/format。

## 实际

- 导入任务 `status=DONE`，`total_rows=3, ok_rows=0, fail_rows=3`；H-84 报告每行原因均为：
  `rpc error: code = InvalidArgument desc = x-row-scope metadata is required`
- 导出 `POST /api/v1/exports` 返回 `{"code":1001}`；worker 任务 payload 记录：
  `"error": "rpc error: code = InvalidArgument desc = x-row-scope metadata is required"`

## 期望

按 TC-140-01/02/09：导入数据正确入库；导出按当前视图范围与命名生成文件。

## 证据

- 结果文件：`.scratch/fullflow-test/p0-api-results.json` → `TC-140-01/02/09`
- 导入报告：`.scratch/fullflow-test/evidence/`（H-84 原文：行号 2/3/4 均报 x-row-scope required）
- worker DB `excel_importexport_tasks.payload` 可见同因错误。
- 根因：worker 的 `importWorkerContext/forwardingContext` 未注入 `x-row-scope`，core records 读/写强制要求该元数据。

## 影响

P0：导入导出两条主链路完全不可用；且导入在失败时仍创建了半成品 Base（「导入中」完成后为空数据），需一并核对回滚/清理策略。
