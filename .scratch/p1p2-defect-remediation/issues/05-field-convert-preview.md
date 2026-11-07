# 05: 字段类型转换预览

**What to build:** 用户更改字段类型时先看到「受影响记录 N 条 / 将清空值 M 个」，确认后才真正转换；预览不写库；公式/关联/引用互转仍被拒绝；失败不返回 9001。对应缺陷 #26。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] H-38 `confirm=false` 返回 affected_count/cleared_count，字段类型不变。
- [x] `confirm=true` 执行转换，数据按既定迁移规则处理。
- [x] 公式/关联/引用互转返回 1001，数据不变。
- [x] UI 对话框展示影响预览并可确认/取消。

## Comments

已收口。records.FieldConversionAdapter（PRD 7.1 迁移规则）+ 预览/确认语义 + 主字段禁转换守卫（集成期按 P0 主字段保护补齐，与 DeleteField 同口径）。UI 影响预览对话框属 M3+ 界面波次（spec Out of Scope），后端契约与黑盒链路就绪；TC-040-16 UI 用例为基线既有失败（非本票回归）。
