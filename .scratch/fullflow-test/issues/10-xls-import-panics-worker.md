# .xls 导入导致 worker 进程 panic 退出（OLE2 空指针）

Status: needs-triage
Severity: P1（可用性/拒绝服务）
Source: TC-140-03（API）

## 复现步骤

1. owner 登录，H-77 上传任意 `.xls`（usage=import_temp）。
2. `POST /api/v1/imports/preview`（sheet_index=0）。

## 实际

- worker 进程崩溃退出，后续所有 worker 依赖接口（上传/导入/导出/通知查询等）统一返回 `9001 服务暂不可用`，直到手工重启 worker。
- worker stderr 栈：

```
panic: runtime error: invalid memory address or nil pointer dereference
[signal 0xc0000005 code=0x0 addr=0x78]
github.com/extrame/ole2.(*Ole).OpenFile(...)
github.com/extrame/xls.OpenReader(...)
excel_worker_service/internal/importexport.parseXLS(...)
```

## 期望

按 TC-140-03：`.xls` 预览/入库与 xlsx 行为一致；即使文件损坏/非标准，也应返回 400/1001/5002，不得使服务进程退出。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-140-03`
- `.scratch/fullflow-test/p1p2-api-run.log`
- worker stderr：`out/worker-p2.err.log`（panic 栈）

## 影响

- 任一用户上传一个特制 `.xls` 即可打垮整个 worker，属于拒绝服务面；同时造成导入/导出/附件链路全量不可用。

## 修复建议

- `parseXLS` 外层 `defer recover()`，panic 转为 1001 业务错误；
- 在调用 `xls.OpenReader` 前做 OLE2 魔数/文件头校验；
- 升级/替换 `extrame/xls`（该库对异常输入容错差），并补充损坏 xls 的回归夹具。
