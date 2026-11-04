# 22: records/write_options.go 按职责拆文件

**What to build:** `excel_core_service/internal/records/write_options.go`（770 行）混装 7 个接口 + 事务助手 + 校验 + proto marshal + oplog + URL 校验，文件名不揭示内容（Mysterious Name）。按职责拆为多个文件（接口定义、事务助手、校验、proto marshal、oplog、URL 校验），包不变、纯移动不改逻辑。验收：拆分后每文件单一职责、命名与内容一致；`go vet` 与全部测试通过；diff 无逻辑变更可证（纯移动）。

**Blocked by:** 20-errorinfo-biz-code（20 先清理 Reason 编码路径，再拆文件减少冲突）

**Status:** ready-for-agent

- [ ] 按职责拆分为多个文件
- [ ] 纯移动，无逻辑变更
- [ ] go vet 与全部测试通过
