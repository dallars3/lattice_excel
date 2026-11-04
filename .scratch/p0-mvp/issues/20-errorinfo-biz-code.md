# 20: ErrorInfo.Reason 业务码字段化

**What to build:** `records/write_options.go:424-436,571` 将业务码编入 `ErrorInfo.Reason` 字符串再 `strconv.Atoi` 解回，改为 proto 新增专用 `int32 biz_code` 字段（向后兼容小契约变更），`Reason` 回归其本来语义。验收：业务码传递不再经过字符串编解码；buf build 通过；接口文档同步；契约测试回归通过。

**Blocked by:** 19-fieldtype-registry（同批重构，避免 records 目录反复翻动）

**Status:** ready-for-agent

- [ ] proto 契约新增 biz_code 字段
- [ ] write_options.go 字符串编解码路径清除
- [ ] 接口文档与契约矩阵同步
- [ ] 契约测试回归通过
