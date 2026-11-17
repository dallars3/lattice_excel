# 01: xls 导入稳定性

**What to build:** 用户导入 `.xls` 时，正常文件与 `.xlsx` 一样完成预览与入库；损坏、加密或非 OLE2 文件只返回 1001 错误信封；任何解析异常都不会让 worker 退出，随后上传、导入、导出仍可用。对应缺陷 #10。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] 正常 `.xls` 能识别列并成功入库 3 行。
- [x] 损坏/伪造 `.xls` 返回 1001，worker 进程仍存活，随后导入与导出接口正常。
- [x] 包级回归覆盖 xls 解析 panic 被 recover，且不依赖进程重启。
- [x] P1/P2 套件中的 xls 用例不再需要「放最后」规避。

## Comments

已收口（集成分支）。保留 extrame/xls（实测替换候选 shakinm/xlsReader 对同源文件同样 panic），四层防御：parseXLS panic recover、OLE2/BIFF 结构校验（xls_ole2.go，含 FAT 链校验封死库内 log.Fatal 路径）、Root Entry 名规范化（SheetJS 产物）、BIFF8 256 列扫描。黑盒 TC-140-03 绿，xls 用例不再需要「放最后」。
