# 05: 字段类型转换预览

**What to build:** 用户更改字段类型时先看到「受影响记录 N 条 / 将清空值 M 个」，确认后才真正转换；预览不写库；公式/关联/引用互转仍被拒绝；失败不返回 9001。对应缺陷 #26。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] H-38 `confirm=false` 返回 affected_count/cleared_count，字段类型不变。
- [ ] `confirm=true` 执行转换，数据按既定迁移规则处理。
- [ ] 公式/关联/引用互转返回 1001，数据不变。
- [ ] UI 对话框展示影响预览并可确认/取消。
