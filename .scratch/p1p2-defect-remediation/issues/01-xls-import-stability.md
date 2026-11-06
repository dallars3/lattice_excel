# 01: xls 导入稳定性

**What to build:** 用户导入 `.xls` 时，正常文件与 `.xlsx` 一样完成预览与入库；损坏、加密或非 OLE2 文件只返回 1001 错误信封；任何解析异常都不会让 worker 退出，随后上传、导入、导出仍可用。对应缺陷 #10。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 正常 `.xls` 能识别列并成功入库 3 行。
- [ ] 损坏/伪造 `.xls` 返回 1001，worker 进程仍存活，随后导入与导出接口正常。
- [ ] 包级回归覆盖 xls 解析 panic 被 recover，且不依赖进程重启。
- [ ] P1/P2 套件中的 xls 用例不再需要「放最后」规避。
