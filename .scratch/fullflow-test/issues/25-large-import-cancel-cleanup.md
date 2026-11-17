# 大文件导入在客户端断连后任务取消且清理失败

Status: needs-triage
Severity: P1
Source: TC-140-08（API，20k 行 CSV）

## 复现步骤

1. 上传 20000 行 CSV（usage=import_temp）。
2. H-93 预览 → H-82 创建导入（新 Base「导入中」）。
3. 客户端轮询 H-83 期间连接中断（浏览器/脚本断开）。

## 实际

- 客户端侧 `fetch failed`；
- worker 日志：
  `ERROR mark import failed task_id=... error="update importexport task: context canceled"`
  `ERROR importexport: cleanup failed import base base_id=... error="rpc error: code = Canceled desc = context canceled"`
- 导入任务被取消，半成品 Base 的清理因同一 ctx 取消而失败。

## 期望

按 TC-140-08 / issue 08 决定：导入失败或中断时必须清理半成品 Base（或保持隔离态可重试），不得留下可访问/半清理的 Base。

## 证据

- `.scratch/fullflow-test/p1p2-api-run.log` → `TC-140-08 fetch failed`
- `out/worker-p2.err.log` → 上述两条 ERROR

## 修复建议

- 清理动作使用独立的、未随请求取消的 context（如 `context.WithoutCancel` + 超时）；
- 导入任务状态机在客户端断连时转为 failed 并可靠触发清理；
- 补充 20k 行导入的中断回归用例。
