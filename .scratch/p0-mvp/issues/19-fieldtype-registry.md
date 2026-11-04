# 19: 字段类型单点注册表（收口 5 处 FieldType switch）

**What to build:** 字段类型元数据（校验规则、计算行为、类型转换）收敛到单一注册表包（建议 `libs/`，worker 的 importexport 也消费），`structure/validation.go:48`、`calc/evaluate.go:123`、`records/query_view.go`、`records/write_options.go`、`importexport/create_import.go` 五处 switch 改为查表；`evaluate.go` 中 `"FIELD_TYPE_TEXT"` 字符串字面量一并替换为 proto 枚举。验收：新增字段类型只改注册表一处 + proto，五处消费点无需感知；全量契约测试与单测通过。

**Blocked by:** 无（独立重构，建议在 p0-mvp 合并后进行）

**Status:** ready-for-agent

- [ ] 建立 fieldtypes 注册表包，含 11 种字段类型的元数据
- [ ] 五处 FieldType switch 全部改为查表
- [ ] evaluate.go 字符串枚举字面量清除
- [ ] 契约测试与单测回归通过
