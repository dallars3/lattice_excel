# 04: 迁移锁重试包装

**What to build:** 测试基建中执行迁移（Up/Down/全量重放）的入口统一包一层锁等待重试（指数退避，如 2s/4s/8s 三次），消除共享 mysqld 高负载下 golang-migrate 硬编码 10 秒锁等待导致的偶发「无法获取锁」失败。仅作用于测试库（票 01 护栏已保证），不改 golang-migrate 驱动本身。

**Blocked by:** None (can start immediately).

**Status:** ready-for-human

- [x] 测试迁移执行入口（Up/Down/重放）统一接入锁等待重试（短退避多次），重试行为有单元用例
- [x] 高负载场景验证：live stack 运行下连续多次全量迁移重放无锁超时失败
- [x] 既有测试全部保持绿；重试耗尽后的失败仍清晰上报（不静默吞错）

## Comments

### 2026-11-20 实现（fix/rc-04-migrate-lock-retry，6e32be4..576d20a）

**实现：**

- base/migrate_retry.go（excel_core_service + excel_worker_service 各一份，零共享新包）：`withMigrationRetry`（初始尝试 + 3 次重试，退避 2s/4s/8s，`migrateRetryDelay` 包变量可注入供单测归零）；锁错误判定 `isMigrationLockError`：`migratedb.ErrLocked`（驱动 GET_LOCK 超时，"can't acquire lock"）、`migrate.ErrLocked`、MySQL 1205（errors.As 链上识别）。重试耗尽原样上抛最后一次错误（保留 `migrate core up: %w` 包装链），不静默吞错。
- 导出面：新增 `MigrateUpWithRetry`/`MigrateDownWithRetry`，直接委托既有 `MigrateUp`/`MigrateDown`（生产实现零改动）；生产 main.go 仍调用无重试入口（已 grep 验证非测试代码仅 migrate_retry.go 定义处引用 WithRetry）。不改 golang-migrate 驱动、不改 go.mod（go-sql-driver/mysql 原已是直接依赖）。
- 调用点切换：全部测试迁移调用点改用 WithRetry——core 32 个 `*_test.go`（30 个 `base.` 前缀 + base 包内 migrate_test.go 全量重放 harness、iam_test.go）、worker 6 个（5 个前缀 + base 包内 migrate_test.go）。共 38 文件。

**测试证据（TDD red→green）：**

- red：两服务 base/migrate_retry_test.go 先于实现（panic 桩），首轮 `go test ./base/` 均 panic("not implemented") 失败。
- green：单元用例 5 项 × 2 服务——2 次锁失败后成功 = 3 次尝试；非锁错误 1 次尝试原样返回；恒失败 = 4 次尝试、最后一次错误逐字上抛（errors.Is 保留 ErrLocked 链）；锁错误判定表驱动（ErrLocked 直返/包装/1205/1213 不重试/1062 不重试/普通错误/nil）；默认退避函数 2s/4s/8s 断言。
- 三模块 `go build ./...` + `go vet ./...` 通过；目标套件 `go test -p 1 -count=1 ./internal/iam/... ./base/...`（core）与 `./base/... ./internal/scheduler/...`（worker）绿。

**高负载验证（live stack 运行中，未停）：**

- `scripts/verify-test-db-isolation.ps1` 连跑两轮：两轮 core 16 包 + worker 9 包全绿，日志中锁相关错误（can't acquire lock / ErrLocked / lock wait timeout）计数为 0——共享 mysqld 高并发下全量迁移重放零锁超时。
- 追加：core/worker `TestMigrationsAreOrderedAndIdempotentOnMySQL -count=3`（每轮含 Up/Up/Down/Down 全量重放）在 live 负载下 3 连跑全绿（176s/146s/165s；worker 111s）。
- **快照对比步骤两轮均 FAIL，属外部干扰非本票缺陷**：差异全部是业务表行数增长（excel_iam_users 397→454→503、excel_bus_outbox 2922→3275 等），且差分诊断证实——不跑任何测试、仅闲置 3 分钟后重放快照对比，共享库同样增长（fields 1303→1318 等）；期间机器上有 3 个 chrome-headless-shell + 5 个 node 进程（并行验收/E2E 正驱动 live stack 写共享 dev 库）。测试库隔离本身无恙：*_test 库守卫全程未触发，业务表增长不可能来自迁移重放（迁移只动 *_test 库的表结构与 schema_migrations）。建议在无并行 E2E 流量的窗口复跑该脚本即可同时绿快照项。
