# 06: 企业与成员管理

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 02 eventbus-outbox；04 iam-auth

**Status:** done

- [x] 未注册用户名可占邀；已属他企业返回 1002；注册后接受加入该企业（单企业模型）
- [x] 停用后全访问权立即失效、其 Base 归 owner；启用恢复原企业成员
- [x] 自我降级与转移最后 owner 被禁止且解释原因
- [x] invite.created/canceled、member.disabled/enabled、enterprise.owner_transferred 事件可观察
