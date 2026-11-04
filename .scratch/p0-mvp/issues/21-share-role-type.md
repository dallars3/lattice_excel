# 21: share 域 Role 类型化

**What to build:** `excel_core_service/internal/share/common.go` 以裸 string 承载 role 的 normalize/compare/cap，引入小 `Role` 类型（含合法值集与比较/cap 方法），入口/出口处与 string 互转。验收：share 域内 role 不再以裸 string 参与 compare/cap 逻辑；单测与契约测试回归通过。

**Blocked by:** 无

**Status:** ready-for-agent

- [ ] Role 类型定义与合法值集
- [ ] normalize/compare/cap 迁移到 Role 方法
- [ ] 单测与契约测试回归通过
