// base/core_schema.go：worker 跨库访问 core 侧 schema 的库名（可测试覆盖）。
package base

// CoreSchemaName 是 worker 跨库访问 core 侧 schema（tracking 埋点表、invite 用户
// 目录）的库名，生产固定为共享 dev 库 excel_core。它是**常量而非可变量**：
// 包级可变状态在并行测试下是竞态温床（双轴评审第 5 项），测试覆盖一律经
// 仓储自身的注入缝完成——
//
//	NotifyRepository.WithCoreSchemaName(name) / TrackingRetentionRepository.WithCoreSchemaName(name)
//
// 测试用 testmysql.TestSchemaName("excel_core") 派生（excel_core_test）注入，
// 用例结束由 t.Cleanup 关闭对应仓储即可，不再触碰包级状态；生产 SQL 永远经
// 仓储字段拼接，不硬编码库名。
const CoreSchemaName = "excel_core"
