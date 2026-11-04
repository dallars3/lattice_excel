# 03: 生命周期到期任务 intake 通道

**What to build:** 用户视角：账号申请注销 7 天到期、企业注销受理 30 天冷静期到期后，系统自动执行生效动作；不再需要人工向任务表插数据。执行仍幂等，重复触发只生效一次。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] 定义并落地明确的 intake 契约：新增事件 E-18 `deactivation.initiated` / E-19 `dissolution.initiated`（payload 含 entity_id、kind、deadline），由 `iam.RequestDeactivate` / `iam.InitiateEnterpriseDissolution` 发布、`worker.scheduler` 消费后写入 tasks 延迟任务（不得破坏 proto 110 条 RPC 计数）——core 事务内 enqueueEvent 发布；worker lifecycle_intake.go 可靠消费写入 SchedulerTask（迁移 000008 加 kind/entity_id/event_id + UNIQUE(kind,entity_id,status)，EnqueueLifecycle ON DUPLICATE KEY 幂等）
- [x] 接口文档与 spec 同步记录该契约（Union 事实源纪律：新事件/字段入 §5/§8 清单）——接口文档 §5（436-437 行）与 §8 负载表（1076-1077 行）、整体技术方案 §6.4（360-361 行）同步
- [x] 通过真实路由测试证明：请求注销/受理销毁 → 到期任务自动出现 → 到期后 ExecuteDeactivation/ExecuteEnterpriseDissolution 被调用且 idempotent——core 事件发布测试（4 新增）+ worker intake 消费测试；到期执行由既有 runner 领取（DeactivationCooloff/EnterpriseDissolution）调用 Execute*
- [x] 重复投递同一 intake 仅创建一个可执行任务（tasks 表 (kind, entity_id, status=pending) 唯一 + event_id 去重），任务失败走既有退避/死信语义——唯一键 + ON DUPLICATE KEY 幂等测试覆盖
- [x] 撤回路径不发事件：CancelDeactivate / CancelEnterpriseDissolution 后，到期 runner 调 Execute* 时由 iam 状态校验兜底（已撤回 → no-op、不入审计）——ExecuteDeactivation/ExecuteEnterpriseDissolution 补撤回 no-op 校验

