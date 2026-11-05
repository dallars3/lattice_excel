# 07: field-description（N3 字段描述，P0 建+读）

**What to build:** 字段对象增 description（长度上限）；建字段入参支持写入说明；查询即返回；未设时回空、向下兼容；编辑通道 P1（本票不做）。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 建字段写入说明，查询读回一致（往返测试）
- [ ] 超长描述被拒绝且错误语义明确
- [ ] 未设描述时返回空值，既有调用不受影响
- [ ] proto 重生成脚本执行通过
- [ ] 文档护栏脚本全绿（RPC 计数保持 111）

## Answer
已落地（N3 P0 建+读）。`Field.description=8`、`CreateFieldRequest.description=5`，上限 500 runes（超长拒绝、错误语义明确）；建字段写入、查询返回、未设回空；复制 Base 路径透传 description 防丢数据；迁移 `000016_field_description.{up,down}.sql`；编辑通道按裁定留 P1。Evidence: merge `93cfd12`。