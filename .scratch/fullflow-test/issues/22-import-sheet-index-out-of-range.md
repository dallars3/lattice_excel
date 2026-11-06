# 导入 sheet_index 越界被静默接受（回落到第 0 个 Sheet）

Status: needs-triage
Severity: P1
Source: TC-140-06（API）

## 复现步骤

1. 上传一个 2 Sheet 的 xlsx / 单 Sheet CSV。
2. `POST /api/v1/imports`，`sheet_index=5`（越界）。

## 实际

接口返回 0，任务 `DONE`，`total_rows=3, ok_rows=3`，实际导入了第 0 个 Sheet。

## 期望

越界 `sheet_index` 应返回 1001 参数错误，不创建导入任务；前端据错误提示修正映射后可用同一 file_id 重试（TC-140-06）。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-140-06`（response 含 `status=DONE, ok_rows=3`）

## 影响

用户以为导入了指定 Sheet，实际导入了别的 Sheet，数据静默错位。
