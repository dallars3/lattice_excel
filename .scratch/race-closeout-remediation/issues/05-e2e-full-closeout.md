# 05: E2E 全量收口

**What to build:** 重建部署（含本批全部修复）后，对部署栈跑全量 E2E 收口：ad-fixes 3 条 + m2-fixes 17 条 + p1p2 全量 54 条 + sf-stress 6 条全绿，作为本批最终验收门。发现缺陷按测试纪律记录，小缺陷直接修复提交，大缺陷报告编排者。

**Blocked by:** 01, 02, 03, 04（全部修复合并进部署分支并重建部署后执行）。

**Status:** done

- [x] 编排者重建部署后（或实现者按部署口径说明自行验证），sf-stress 6 条全绿
- [x] ad-fixes 3 条与 m2-fixes 17 条对部署栈回归绿
- [x] p1p2 全量 54 条通过或有据跳过（票 02 triage 结论落地）
- [x] 邀请流全链路用例（票 03 交付的 Go 集成用例）回归绿
- [x] 限速合规、幂等可重跑（沿用既有账号前缀与守卫策略）
- [x] 发现缺陷按测试纪律记录；小缺陷直接修复提交，大缺陷报告编排者

## Comments

### 2026-11-20 收口（部署自 fix/race-closeout-remediation @ rc-int，最终 182bb4d）

**重建部署：** generate-proto → build-all（前端构建→go:embed→go build ./...）→ 三 exe 入 rc-int/out → core→worker→web 重启（deploy-race-closeout.ps1），healthz/readyz 200，最终服务 bundle index.nxMxGUcP.js（评审修复后）。:8080/:9091/:9092 全部由集成构建承载。

**验收门结果（日志 rc-int/out/closeout-*.log 与 reviewfix-*.log）：**
- Go core 全量 `go test -p 1 ./...`：15/15 包绿（含票 03 全链路用例）；worker 全量绿。
- sf-stress **6/6** ×2（初跑 + 幂等复跑）；ad-fixes **3/3**（首跑 1 失败为环境级：Go 套件 flushdb 清掉共享 Redis 消费组 → N5 回填 calc.job 无人消费；XGROUP CREATE 修复消费后复跑绿）；m2-fixes **17/17**；p1p2 **51 过 / 3 跳（有据 fixme）/ 0 败**；p0 **31/31**。
- 限速合规（套件内 15s 重试 + 套件间 ≥75-80s 冷却，全程串行）、幂等可重跑（sf-stress 复跑 + 各套件重跑绿）实证。

**收口期小缺陷修复（票面授权直接提交）：**
- 9a88ebc（产品）：TC-WM2F-11 确定性失败——列头较单元格右偏 4px，根因 UI 批次 7566a40 给表头条带加了 4px 水平内边距（违反 BUG-03 x-offset=0 契约）→ 去除水平内边距；连带治愈此前挂起的 TC-060-07 冻结列 4px 漂移（同根因），p0 恢复 31/31。
- e10110d（修用例）：TC-060-06 恢复字段显隐未等待落库即关页（服务端 hidden_field_list 残留致 TC-180-03 连锁失败，两轮 DB 实证）→ 落库轮询收口；产品自动保存契约未动。

**发现并记录的遗留（建议独立票，未在本批处理）：**
1. 共享 Redis flushdb 清消费组：Go 套件与 live stack 共用 Redis，flush 后 stream 消费组全灭、calc/notify/tracking/audit 消费停摆（本次已手动 XGROUP CREATE 恢复）。建议消费端 re-EnsureGroup 或评估测试禁用 flush（spec Out of Scope 预留项）。
2. 偶发网格停滞 flake：7 轮全量跑中 3 次（TC-060-04/TC-060-10×2/p0 TC-050-09），写后记录查询最长 120s 渲染不出新数据；单跑与重跑必绿。疑 cancel+invalidate 突发边界（评审修复 094dadf 的 await 串行化疑似缓解），值得 trace 采集专项。
3. p0 TC-050-01 用例体基线计数竞态（before 计数早于首屏记录载入，预期 1 实得 5）：建议按时序确定化模式修正。
4. 弹层编辑器提交不收起（TC-040-07/09/14 三条 fixme 同因，:8080 A/B 既有）。
5. 收口期曾观察到疑似并行重复 E2E 执行痕迹（同秒账号后缀碰撞 ad05ownnzagwm、额外 m2/p1p2 Base）；编排侧已核查无残留进程，疑为代理工具调用重试副作用，无数据损害，记录备查。
