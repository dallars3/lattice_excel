# 01: 测试 Redis 隔离护栏（prefactor）

**What to build:** 为核心与 worker 的 Go 测试提供统一、默认安全的 Redis 隔离入口。测试只能使用专用逻辑库或独立 Redis 实例；任何清理动作在连接或执行前校验目标，发现仍指向 live stack 时直接失败。完成后，在 live stack 运行状态下执行全量 Go 测试不会删除现有 Streams 消费组或影响业务消费链路。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] 测试 Redis 入口支持独立的逻辑库或专用实例，默认目标与 live stack 不同；受限部署可经显式配置切换到专用测试实例
- [ ] 不安全的共享目标在任何清理动作前硬失败，报错明确指出测试 Redis 与 live stack 未隔离；不得静默回退到共享库
- [ ] 核心与 worker 测试中的 session、登录锁、Streams、EventBus 等辅助客户端统一通过隔离入口创建，测试代码不再直接清理共享 Redis
- [ ] live stack 运行状态下执行核心与 worker 全量测试，现有 Streams 消费组与 pending 状态前后保持可消费，calc/notify/tracking/audit 等消费者未停摆
- [ ] 既有 Go 测试全部保持绿，隔离入口的正反路径有自动化覆盖

