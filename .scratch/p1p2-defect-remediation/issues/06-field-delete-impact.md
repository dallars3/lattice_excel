# 06: 删除字段影响面与级联清理

**What to build:** 用户删除字段前看到影响面（记录数、依赖公式数、依赖引用数），确认后真正删除，并自动清理视图筛选/隐藏/冻结/排序/分组/宽度中的引用；删除后字段不可达 4002。对应缺陷 #27。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] H-39 `confirm=false` 返回 record_count/dependent_formulas/dependent_lookups。
- [ ] `confirm=true` 删除成功，视图配置不残留该字段引用。
- [ ] 删除后查询该字段/记录返回 4002 或相称错误。
- [ ] 主字段删除仍被拒；业务失败不返回 9001。
