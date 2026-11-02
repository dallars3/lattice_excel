# 11: calc 公式引擎与重算

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 01 bootstrap；02 eventbus-outbox

**Status:** done

- [x] 26 个函数逐用例正确（含 TODAY/NOW/日期函数）
- [x] 除零与空值语义符合约定，#ERROR 附原因
- [x] 含关联-引用间接环的循环依赖在保存期被拒绝
- [x] 跨行重算任务幂等消费、物化写回并再次广播
