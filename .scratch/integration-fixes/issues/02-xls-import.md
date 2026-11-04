# 02: .xls 导入解析补全

**What to build:** 用户视角：导入向导选择 .xls 文件时获得与 .xlsx/.csv 同等的体验——预览能识别列类型与首行字段名提示，确认后逐行入库，坏行跳过并产出可下载的错误报告，完成后收到导入完成通知。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] PreviewImport 对 .xls 正确解析并给出 detected_type 与 first_row_header_hint，行为与 .xlsx 用例一致
- [ ] CreateImport 对 .xls 完成全部行写入、部分失败续行、报告文件生成
- [ ] 对应场景发布 import.finished（字段齐全含企业标识，供通知与审计消费）
- [ ] 新增 .xls 契约测试与既有 xlsx/csv 测试共存，三服务回归 exit 0

