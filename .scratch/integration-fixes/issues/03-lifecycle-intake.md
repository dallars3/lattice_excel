# 03: 生命周期到期任务 intake 通道

**What to build:** 用户视角：账号申请注销 7 天到期、企业注销受理 30 天冷静期到期后，系统自动执行生效动作；不再需要人工向任务表插数据。执行仍幂等，重复触发只生效一次。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 定义并落地明确的 intake 契约（事件或既有接口内新增字段，不得破坏 proto 110 条 RPC 计数），使注销/企业销毁的到期信息能从 core 到达 worker 任务表并被 runner 领取执行
- [ ] 接口文档与 spec 同步记录该契约（Union 事实源纪律：新事件/字段入 §5/§8 清单）
- [ ] 通过真实路由测试证明：请求注销/受理销毁 → 到期任务自动出现 → 到期后 ExecuteDeactivation/ExecuteEnterpriseDissolution 被调用且 idempotent
- [ ] 重复投递同一 intake 仅创建一个可执行任务，任务失败走既有退避/死信语义

