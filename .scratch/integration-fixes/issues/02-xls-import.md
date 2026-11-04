# 02: .xls 导入解析补全

**What to build:** 用户视角：导入向导选择 .xls 文件时获得与 .xlsx/.csv 同等的体验——预览能识别列类型与首行字段名提示，确认后逐行入库，坏行跳过并产出可下载的错误报告，完成后收到导入完成通知。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] PreviewImport 对 .xls 正确解析并给出 detected_type 与 first_row_header_hint，行为与 .xlsx 用例一致——parseImportFile 增 "xls" 分支，github.com/extrame/xls 解析喂 buildParsedTable，xls_test.go 断言 preview 类型/首行提示与 xlsx 同构
- [x] CreateImport 对 .xls 完成全部行写入、部分失败续行、报告文件生成——走既有 parseImportFile/create_import 链路，mixed.xls fixture 覆盖全量成功与坏行续行+报告
- [x] 对应场景发布 import.finished（字段齐全含企业标识，供通知与审计消费）——复用 publishImportFinished（enterprise_id/base_id 等），xls 用例断言事件发布
- [x] 新增 .xls 契约测试与既有 xlsx/csv 测试共存，三服务回归 exit 0——xls_test.go 3 测试 + testdata/Table.xls、mixed.xls；最终 contract-test 全量回归 exit 0

