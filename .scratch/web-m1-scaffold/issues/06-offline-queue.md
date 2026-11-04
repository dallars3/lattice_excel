# 06: 离线队列 store 状态机（ADR 0002 语义）

**What to build:** 离线编辑的队列状态机（纯 store，M1 不与真网络联动）：断线只读、在途编辑保留入队、重连后先补增量再顺序重放、冲突转三动作裁决、任何失败项内容不丢失可复制、滞留超 5 分钟触发未同步清单提示流。语义来自 ADR 0002 与需求文档 4.6 离线模型。

**Blocked by:** 01: 工程底座

**Status:** ready-for-human

- [x] 全状态迁移表驱动测试绿（入队/断线只读/重放中/冲突/全部完成/滞留）
- [x] 断线状态下新编辑被拒（离线只读），已入队项保留
- [x] 冲突项可被转交三动作裁决（use_other/overwrite/copy），失败项内容可复制不丢失
- [x] 队首滞留超 5 分钟触发「未同步清单 + 复制入口」提示流（计时器可注入）
- [x] 多标签各自独立队列（store 实例隔离语义可测）

## Comments

- feat commit：`d3f8e84`（分支 feature/m1-web）。
- 落点：`web/src/core/offline/queue.ts`（纯状态机，DOM 无关）+ `web/src/core/offline/index.ts` 门面转正 + `web/src/stores/offlineQueue.ts`（zustand 接线；技术方案第 8 章消费方 banner/grid）。plan §4 落点两文件之外补门面 `index.ts`（原票 01 占位注释转真实出口）。
- 状态机（时钟全部注入 Scheduler（复用票 05 接缝/manual-clock）、发送全部注入 sender，M1 零真实网络、测试零真实等待）：
  - 相位迁移 idle → offline → replaying → idle 全表驱动（5 组语义区 27 用例）；状态经 getSnapshot/subscribe 可订阅（相位序列与重放进度序列均有断言）。
  - 入队：写提交先落队列（队列项字段齐备：client_request_id 复用票 04 生成器口径、目标资源 target、编辑前快照、本地值、状态、毫秒时间戳），网络正常即发（queued→sending→synced），严格 FIFO 单飞派发（flushPending 收口保证测试确定性）。
  - 断线只读：goOffline 后新编辑明确拒绝（返回 `{accepted:false, reason:'offline_readonly'}`，不静默吞）；在途 sending 项与未派发 queued 项保留，在途结果照常回填（ADR 0002「在途项保留」）。
  - 重放：reconnect 先 await 补增量钩子（onReconnectSync，ADR 0002「先补增量再顺序重放」的顺序约定，M1 不真拉增量）再顺序 flush；重放进度 `{done,total}` 会话口径逐条推进可订阅；failed 项原位保序停止本段派发、下次触发以同一幂等 id 重试；补增量钩子失败则会话中止回 offline 且 reconnect 明确抛错；全部完成回 idle、进度清空。
  - 冲突（4001）：项转 conflict 并记录 serverValue，派发暂停；resolveConflict 三动作裁决——use_other（采用服务端值、丢弃本地编辑）/ overwrite（同幂等 id 重发、sender 收 `overwrite:true`，M3 携远端 version）/ copy（另存为副本、原项标记冲突已处理）；裁决口只认冲突态（非冲突项/未知 id 抛错不静默）。
  - 失败项内容不丢失：snapshot+localValue 恒保留，`getItemContent` 为「未同步清单复制入口」的事实源；冲突队首同样计入滞留判定。
  - 滞留：队首未结算项滞留 >5 分钟（`STALL_THRESHOLD_MS=300000`，阈值可注入）触发一次「未同步清单（全部未结算项、入队序）+ 复制入口」提示流，以事件/回调表达（onStall 选项 + addStallListener 双通道，M1 无 UI，UI 消费是后续票/M3 的事）；队首结算后新 episode 重新武装、同队首不重复提示。
- store 接线与多标签隔离取舍：跨标签页天然是独立 JS 运行时（每标签各一份模块实例与队列），页内保留 `offlineQueueStore` 单例供 banner/grid 直接 import 消费（技术方案第 8 章挂载点，票 02 骨架转正）；实例隔离语义由工厂 `createOfflineQueueStore` 承载（sender/scheduler/generateRequestId/stallThresholdMs/onStall 全注入面），「两个 store 实例互不可见对方队列、相位互不影响」用例锁死；未装配 sender 时的占位实现返回显式 error 结果，绝不静默成功。
- 验证：`npm run test` 294/294 绿（基线 258 不回归，新增 36：core/offline 表驱动 27 + stores 接线 9）；`npm run build` 成功（tsc --noEmit 无类型错误），JS chunk 4 ≤6（offline 未进演示页构建图，M3 接线后按既有 manualChunks 归组）；TDD 红-绿逐片推进（五片均先确认红态再实现转绿）。
- 偏差：
  - `stores.test.tsx`（票 02）中 offlineQueueStore 初始状态期望由 `{pendingItems, replayProgress}` 更新为 `{phase, items, pendingItems, replayProgress}`——本票填充真实状态机后初始状态面变化，属骨架转正的必要同步（改的是测试文件，装配层 stores/index.ts 仅做类型再出口，未动挂载结构）。
  - M1 不与真网络联动（spec）：sender 全部经 Fake 注入；onReconnectSync 仅表达「先补增量」顺序约定，不真发 W-07 replay_request（真网络联动在 M3，换实现零改状态机）。
