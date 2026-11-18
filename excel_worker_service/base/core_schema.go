// base/core_schema.go：worker 跨库访问 core 侧 schema 的库名（可测试覆盖）。
package base

// CoreSchemaName 是 worker 跨库访问 core 侧 schema（tracking 埋点表、invite 用户
// 目录）的库名。生产固定为共享 dev 库 excel_core。
//
// 2026-11-18 数据事故护栏（规则见 excel-libs/testmysql）：worker 测试一律跑在
// excel_worker_test，其跨库引用必须同步指向 core 测试库——测试在临时改动本变量时
// 用 testmysql.TestSchemaName("excel_core") 派生（excel_core_test），并 t.Cleanup
// 还原；生产 SQL 永远经本变量拼接，不硬编码库名。
var CoreSchemaName = "excel_core"
