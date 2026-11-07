# 14: 外链清单元数据

**What to build:** 企业管理员查看「任何人」外链清单时，每条记录包含 Base 名称与责任管理者用户名，可与强关操作使用同一 Base 标识。对应缺陷 #16。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] H-71 条目 `base_name`、`manager`、`role`、`opened_at` 均非空。
- [x] 管理员强关后在线外部会话在 1 分钟内失效。
- [x] 非管理员访问清单返回 3001。
- [x] 强关动作只落一条 LINK_FORCE_CLOSED 审计。

## Comments

已收口。H-71 base_name/manager/role/opened_at 在 core share 组装层补齐（BaseNameProvider seam），强关寻址/3001/单审计不变；TC-130-05 绿。
