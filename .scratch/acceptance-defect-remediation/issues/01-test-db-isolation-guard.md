# 01: 测试库隔离护栏（prefactor，数据事故护栏）

**What to build:** 核心与 worker 服务的后端测试一律使用独立测试库：测试框架按固定后缀（如 `excel_core_test`/`excel_worker_test`）自建库、跑迁移、测试结束后清理；现行「清空当前库全部表」的复位路径改为仅作用于测试库，并在执行任何清库/迁移操作前硬校验目标库名带测试后缀，非测试库直接失败并给出清晰报错。护栏落地后，在 live stack 运行状态下跑全量核心测试不再影响共享 dev 库。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] 核心服务测试基建改为独立测试库模式：自建（缺表/缺库时）→ 迁移 → 用例 → 清理
- [ ] worker 服务测试基建同口径
- [ ] 任何清库/迁移 helper 在目标库名不带测试后缀时硬失败（含集成测试复位路径），报错指明原因
- [ ] live stack 运行状态下跑核心全量 go test：共享 dev 库（excel_core/excel_worker）的表行数与内容前后不变（用计数断言验证）
- [ ] 既有测试全部保持绿（切换测试库后语义不变）
- [ ] 用例与护栏行为记录进测试基建文档注释（防下一个人再踩）
