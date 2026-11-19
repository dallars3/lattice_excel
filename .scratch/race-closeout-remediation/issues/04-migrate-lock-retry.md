# 04: 迁移锁重试包装

**What to build:** 测试基建中执行迁移（Up/Down/全量重放）的入口统一包一层锁等待重试（指数退避，如 2s/4s/8s 三次），消除共享 mysqld 高负载下 golang-migrate 硬编码 10 秒锁等待导致的偶发「无法获取锁」失败。仅作用于测试库（票 01 护栏已保证），不改 golang-migrate 驱动本身。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] 测试迁移执行入口（Up/Down/重放）统一接入锁等待重试（短退避多次），重试行为有单元用例
- [ ] 高负载场景验证：live stack 运行下连续多次全量迁移重放无锁超时失败
- [ ] 既有测试全部保持绿；重试耗尽后的失败仍清晰上报（不静默吞错）
