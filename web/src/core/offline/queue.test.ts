// 离线队列状态机表驱动测试（票 06，ADR 0002 / 技术方案 5.3 / 需求文档 4.6 离线模型）。
// 全部时钟经 ManualClock 注入、全部发送经 Fake sender 注入——零真实等待、零真实网络。
import { describe, expect, it } from 'vitest'

import { ManualClock } from '@/test/manual-clock'

import {
  OfflineQueueEngine,
  STALL_THRESHOLD_MS,
  type OfflineQueueEngineOptions,
  type OfflineQueueItem,
  type OfflineQueueTarget,
  type SendResult,
  type StallNotification,
} from './queue'

// ---------- Fake sender：脚本化应答 / 可扣留在途 ----------

interface SendCall {
  readonly item: OfflineQueueItem
  readonly overwrite: boolean
}

class FakeSender {
  readonly calls: SendCall[] = []
  private script: SendResult[] = []
  private hold = false
  private readonly held: Array<(result: SendResult) => void> = []

  /** 预设应答脚本：依次弹出；不设则默认成功。 */
  respondWith(...results: SendResult[]): this {
    this.script.push(...results)
    return this
  }

  /** 扣留模式：send 返回未决 promise，测试用 settleNext 手动回填（模拟在途）。 */
  holdNext(): this {
    this.hold = true
    return this
  }

  /** 解除扣留：后续 send 直接按脚本应答（模拟在途请求结束后恢复自动应答）。 */
  resumeAuto(): this {
    this.hold = false
    return this
  }

  send = (item: OfflineQueueItem, options: { overwrite: boolean }): Promise<SendResult> => {
    this.calls.push({ item, overwrite: options.overwrite })
    if (this.hold) {
      return new Promise<SendResult>((resolve) => this.held.push(resolve))
    }
    return Promise.resolve(this.script.shift() ?? { kind: 'success' })
  }

  /** 手动回填最早一笔在途发送。 */
  settleNext(result: SendResult): void {
    const resolve = this.held.shift()
    if (resolve === undefined) {
      throw new Error('FakeSender：没有在途发送可回填')
    }
    resolve(result)
  }

  /** 当前扣留中的在途发送笔数。 */
  pendingCount(): number {
    return this.held.length
  }
}

/** 微任务级等待（零真实等待）：直到 sender 出现扣留中的在途发送（下一条已进入 sending）。 */
async function untilInFlight(sender: FakeSender): Promise<void> {
  for (let i = 0; i < 50; i += 1) {
    if (sender.pendingCount() > 0) {
      return
    }
    await Promise.resolve()
  }
  throw new Error('sender 在途未按预期出现（微任务内）')
}

// ---------- Harness ----------

interface Harness {
  engine: OfflineQueueEngine
  clock: ManualClock
  sender: FakeSender
  sent(): SendCall[]
}

function createEngine(options?: Omit<OfflineQueueEngineOptions, 'sender'>): Harness {
  const clock = new ManualClock()
  const sender = new FakeSender()
  let idSeq = 0
  const engine = new OfflineQueueEngine({
    sender: sender.send,
    scheduler: clock,
    generateRequestId: () => `req-${(idSeq += 1)}`,
    ...options,
  })
  return { engine, clock, sender, sent: () => sender.calls }
}

function cellTarget(record: string, field: string, value: string): OfflineQueueTarget {
  return { kind: 'update_cell', url: `/bases/b1/tables/t1/records/${record}/cells/${field}`, body: { value } }
}

/** 在线入队并等派发收口（sender 立即应答时 flush 在微任务内结束——零真实等待）。 */
async function enqueueAndSettle(h: Harness, localValue: string): Promise<ReturnType<OfflineQueueEngine['enqueue']>> {
  const outcome = enqueueOnly(h, localValue)
  await h.engine.flushPending()
  return outcome
}

/** 在线入队但不等收口（sender 扣留在途时用——flushPending 会等在途 promise，不能盲等）。 */
function enqueueOnly(h: Harness, localValue: string): ReturnType<OfflineQueueEngine['enqueue']> {
  return h.engine.enqueue({
    target: cellTarget('r1', 'f_name', localValue),
    snapshot: 'before',
    localValue,
  })
}

/** 从快照读某队列项的当前状态（入队返回值是入队时刻快照，结算后状态以 getSnapshot 为准）。 */
function finalItem(h: Harness, clientRequestId: string): OfflineQueueItem {
  const item = h.engine.getSnapshot().items.find((candidate) => candidate.clientRequestId === clientRequestId)
  if (item === undefined) {
    throw new Error(`队列项不存在：${clientRequestId}`)
  }
  return item
}

// ---------- 语义区 1：入队（网络正常即发/在途） ----------

describe('core/offline 状态迁移（表驱动）：入队——网络正常即发', () => {
  interface EnqueueCase {
    name: string
    run: (h: Harness) => Promise<void>
  }

  const CASES: EnqueueCase[] = [
    {
      name: '在线入队：项即发（sender 收到载荷），结算为 synced，phase 保持 idle',
      run: async (h) => {
        const outcome = await enqueueAndSettle(h, '本地值A')

        expect(outcome.accepted).toBe(true)
        expect(h.sent()).toHaveLength(1)
        expect(h.sent()[0].item.localValue).toBe('本地值A')
        expect(h.sent()[0].overwrite).toBe(false)
        expect(finalItem(h, 'req-1').status).toBe('synced')
        expect(h.engine.getSnapshot().phase).toBe('idle')
      },
    },
    {
      name: '队列项字段齐备：client_request_id（票 04 生成器口径）/目标资源/编辑前快照/本地值/状态/时间戳',
      run: async (h) => {
        const beforeMs = h.clock.now()
        const outcome = await enqueueAndSettle(h, 'B')

        if (!outcome.accepted) {
          throw new Error('在线入队应被接受')
        }
        expect(outcome.item.clientRequestId).toBe('req-1')
        const item = finalItem(h, 'req-1')
        expect(item.target).toStrictEqual(cellTarget('r1', 'f_name', 'B'))
        expect(item.snapshot).toBe('before')
        expect(item.localValue).toBe('B')
        expect(item.status).toBe('synced')
        expect(item.enqueuedAtMs).toBe(beforeMs)
        expect(item.updatedAtMs).toBe(beforeMs)
      },
    },
    {
      name: '在线入队：结算后 pendingItems 清空（全部完成态可见）',
      run: async (h) => {
        await enqueueAndSettle(h, 'C')

        const snap = h.engine.getSnapshot()
        expect(snap.items).toHaveLength(1)
        expect(snap.pendingItems).toHaveLength(0)
        expect(snap.replayProgress).toBeNull()
      },
    },
    {
      name: '多条在线入队：按入队顺序逐条发送（FIFO）',
      run: async (h) => {
        h.engine.enqueue({ target: cellTarget('r1', 'f_a', '1'), snapshot: null, localValue: '1' })
        h.engine.enqueue({ target: cellTarget('r2', 'f_b', '2'), snapshot: null, localValue: '2' })
        await h.engine.flushPending()

        expect(h.sent().map((c) => c.item.localValue)).toStrictEqual(['1', '2'])
        expect(h.engine.getSnapshot().pendingItems).toHaveLength(0)
      },
    },
  ]

  it.each(CASES)('$name', async ({ run }) => {
    const h = createEngine()
    await run(h)
  })
})

// ---------- 语义区 2：断线只读（需求 4.6 离线模型） ----------

describe('core/offline 状态迁移（表驱动）：断线只读', () => {
  interface OfflineCase {
    name: string
    run: (h: Harness) => Promise<void>
  }

  const CASES: OfflineCase[] = [
    {
      name: 'goOffline：idle → offline（相位迁移经 subscribe 可感知）',
      run: async (h) => {
        const phases: string[] = []
        h.engine.subscribe(() => phases.push(h.engine.getSnapshot().phase))

        h.engine.goOffline()

        expect(h.engine.getSnapshot().phase).toBe('offline')
        expect(phases).toStrictEqual(['offline'])
      },
    },
    {
      name: '断线新编辑被明确拒绝（offline_readonly），既有队列项保留不受影响',
      run: async (h) => {
        h.sender.holdNext()
        enqueueOnly(h, '在途项')
        h.engine.goOffline()
        const before = h.engine.getSnapshot()

        const outcome = h.engine.enqueue({ target: cellTarget('r9', 'f_x', '新编辑'), snapshot: null, localValue: '新编辑' })

        expect(outcome).toStrictEqual({ accepted: false, reason: 'offline_readonly' })
        expect(h.engine.getSnapshot().items).toStrictEqual(before.items)
        expect(h.sent()).toHaveLength(1) // 新编辑未发
      },
    },
    {
      name: '断线在途项保留：sending 态不被清除，sender 回填后结果照常落账（success → synced）',
      run: async (h) => {
        h.sender.holdNext()
        enqueueOnly(h, '在途项')
        h.engine.goOffline()

        expect(finalItem(h, 'req-1').status).toBe('sending')
        h.sender.settleNext({ kind: 'success' })
        await h.engine.flushPending() // 等在途回填收口（微任务级，零真实等待）
        expect(finalItem(h, 'req-1').status).toBe('synced')
      },
    },
    {
      name: '断线未发送项保留：在途 sending + 未派发 queued 都不丢失，pendingItems 顺序 = 入队顺序',
      run: async (h) => {
        h.sender.holdNext()
        enqueueOnly(h, '1') // 在途
        enqueueOnly(h, '2') // 单飞循环在途等待，未派发（attempts 0）
        h.engine.goOffline()

        expect(finalItem(h, 'req-1').status).toBe('sending')
        expect(finalItem(h, 'req-2')).toMatchObject({ status: 'queued', attempts: 0 })
        expect(h.sent()).toHaveLength(1)
        expect(h.engine.getSnapshot().pendingItems.map((item) => item.localValue)).toStrictEqual(['1', '2'])
      },
    },
  ]

  it.each(CASES)('$name', async ({ run }) => {
    const h = createEngine({ onReconnectSync: () => {} })
    await run(h)
  })
})

// ---------- 语义区 3：重放中（重连后先补增量再顺序 flush）与全部完成 ----------

describe('core/offline 状态迁移（表驱动）：重连重放', () => {
  interface ReconnectCase {
    name: string
    run: (h: Harness) => Promise<void>
  }

  const CASES: ReconnectCase[] = [
    {
      name: '重放顺序：先补增量（onReconnectSync）再 FIFO 顺序重放（failed 项原位重发），全部完成回 idle',
      run: async (h) => {
        const order: string[] = []
        let idSeq = 0
        h.sender.holdNext()
        const engine = new OfflineQueueEngine({
          sender: (item, options) => {
            order.push(`send:${String(item.localValue)}`)
            return h.sender.send(item, options)
          },
          onReconnectSync: () => {
            order.push('sync')
          },
          scheduler: h.clock,
          generateRequestId: () => `req-${(idSeq += 1)}`,
        })
        engine.enqueue({ target: cellTarget('r1', 'f_a', '1'), snapshot: null, localValue: '1' })
        engine.enqueue({ target: cellTarget('r2', 'f_b', '2'), snapshot: null, localValue: '2' })
        h.sender.settleNext({ kind: 'error', message: '断线' }) // 第 1 条失败 → 保序停止
        await engine.flushPending()
        engine.goOffline()
        order.length = 0

        const phases: string[] = []
        engine.subscribe(() => phases.push(engine.getSnapshot().phase))
        h.sender.resumeAuto().respondWith({ kind: 'success' }, { kind: 'success' })
        const replaying = engine.reconnect()
        expect(order).toStrictEqual(['sync']) // 补增量先于任何队列重放，且无 send 越前
        await replaying

        expect(order).toStrictEqual(['sync', 'send:1', 'send:2'])
        expect(phases[0]).toBe('replaying')
        expect(phases[phases.length - 1]).toBe('idle')
        expect(engine.getSnapshot().phase).toBe('idle')
        expect(engine.getSnapshot().replayProgress).toBeNull()
        expect(engine.getSnapshot().pendingItems).toHaveLength(0)
      },
    },
    {
      name: '重放进度可订阅：replaying 期间 replayProgress 从 {0,total} 逐条推进，完成清空',
      run: async (h) => {
        h.sender.holdNext()
        enqueueOnly(h, '1')
        enqueueOnly(h, '2')
        h.engine.goOffline()

        const progresses: Array<unknown> = []
        h.engine.subscribe(() => progresses.push(h.engine.getSnapshot().replayProgress))
        const replaying = h.engine.reconnect()
        h.sender.settleNext({ kind: 'success' }) // 第 1 条成功
        await untilInFlight(h.sender) // 第 2 条进入在途（微任务级）
        h.sender.settleNext({ kind: 'success' }) // 第 2 条成功
        await replaying

        expect(progresses[0]).toStrictEqual({ done: 0, total: 2 })
        expect(progresses).toContainEqual({ done: 1, total: 2 })
        expect(h.engine.getSnapshot().replayProgress).toBeNull()
        expect(h.engine.getSnapshot().phase).toBe('idle')
      },
    },
    {
      name: '空队列重连：仅补增量，相位 replaying → idle',
      run: async (h) => {
        let syncCalls = 0
        const engine = new OfflineQueueEngine({
          sender: h.sender.send,
          onReconnectSync: () => {
            syncCalls += 1
          },
          scheduler: h.clock,
          generateRequestId: () => 'req-x',
        })
        engine.goOffline()

        await engine.reconnect()

        expect(syncCalls).toBe(1)
        expect(engine.getSnapshot().phase).toBe('idle')
      },
    },
    {
      name: '重放中失败：failed 项保序停止本段派发，后续项不越过；再次重连先重试 failed（同幂等 id）',
      run: async (h) => {
        h.sender.holdNext()
        enqueueOnly(h, '1')
        enqueueOnly(h, '2')
        h.sender.settleNext({ kind: 'error', message: '网络抖动' }) // 第 1 条失败 → 保序停止
        await h.engine.flushPending() // 等失败落账收口（微任务级，零真实等待）
        expect(finalItem(h, 'req-1').status).toBe('failed')
        expect(finalItem(h, 'req-2').attempts).toBe(0)
        h.engine.goOffline()
        h.sender.resumeAuto().respondWith({ kind: 'success' }, { kind: 'success' })
        await h.engine.reconnect()

        expect(finalItem(h, 'req-1').status).toBe('synced')
        expect(finalItem(h, 'req-1').attempts).toBe(2)
        expect(finalItem(h, 'req-1').clientRequestId).toBe('req-1')
        expect(h.sent().map((call) => call.item.localValue)).toStrictEqual(['1', '1', '2'])
        expect(h.engine.getSnapshot().pendingItems).toHaveLength(0)
      },
    },
    {
      name: '重放中再次断线：phase offline，派发暂停，在途项保留；恢复后继续重放',
      run: async (h) => {
        h.sender.holdNext()
        enqueueOnly(h, '1')
        enqueueOnly(h, '2')
        h.engine.goOffline()

        const replaying = h.engine.reconnect() // replaying，第 1 条在途
        expect(h.engine.getSnapshot().phase).toBe('replaying')
        h.engine.goOffline() // 重放中再断
        expect(h.engine.getSnapshot().phase).toBe('offline')
        expect(finalItem(h, 'req-1').status).toBe('sending')

        h.sender.settleNext({ kind: 'success' }) // 在途回填
        await replaying
        expect(finalItem(h, 'req-2').status).toBe('queued') // 断线暂停派发

        h.sender.resumeAuto()
        await h.engine.reconnect() // 恢复重放
        expect(finalItem(h, 'req-2').status).toBe('synced')
        expect(h.engine.getSnapshot().phase).toBe('idle')
      },
    },
    {
      name: '补增量钩子失败：重放会话中止回 offline、队列保留，reconnect 明确抛错（不静默吞）',
      run: async (h) => {
        h.sender.holdNext()
        const engine = new OfflineQueueEngine({
          sender: h.sender.send,
          onReconnectSync: () => Promise.reject(new Error('补增量失败')),
          scheduler: h.clock,
          generateRequestId: () => 'req-x',
        })
        engine.enqueue({ target: cellTarget('r1', 'f_a', '1'), snapshot: null, localValue: '1' })
        engine.goOffline()

        await expect(engine.reconnect()).rejects.toThrow('补增量失败')
        expect(engine.getSnapshot().phase).toBe('offline')
        expect(engine.getSnapshot().pendingItems).toHaveLength(1) // 在途项保留，等待下次重连
      },
    },
  ]

  it.each(CASES)('$name', async ({ run }) => {
    const h = createEngine({ onReconnectSync: () => {} })
    await run(h)
  })
})

// ---------- 语义区 4：冲突三动作裁决（FR-06-3 / 4001）与失败项内容不丢失 ----------

describe('core/offline 状态迁移（表驱动）：冲突三动作', () => {
  /** 造出冲突前置态：第 1 条 4001 冲突、第 2 条 queued 未发。 */
  async function buildConflictHead(h: Harness, serverValue: string): Promise<void> {
    h.sender.respondWith({ kind: 'conflict', serverValue })
    enqueueOnly(h, '1')
    await h.engine.flushPending()
    enqueueOnly(h, '2') // 冲突暂停后入队，不越过
  }

  interface ConflictCase {
    name: string
    run: (h: Harness) => Promise<void>
  }

  const CASES: ConflictCase[] = [
    {
      name: '4001 冲突：项转 conflict 并记录 serverValue，flush 暂停，后续项不越过（严格 FIFO）',
      run: async (h) => {
        await buildConflictHead(h, '服务端值X')

        expect(finalItem(h, 'req-1')).toMatchObject({ status: 'conflict', serverValue: '服务端值X', attempts: 1 })
        expect(finalItem(h, 'req-2')).toMatchObject({ status: 'queued', attempts: 0 })
        expect(h.engine.getSnapshot().phase).toBe('idle')
        expect(h.sent()).toHaveLength(1)
      },
    },
    {
      name: 'use_other：采用服务端值丢弃本地编辑（resolution 落账），冲突解除后续跑重放',
      run: async (h) => {
        await buildConflictHead(h, '服务端值X')
        h.sender.respondWith({ kind: 'success' })

        h.engine.resolveConflict('req-1', 'use_other')
        await h.engine.flushPending()

        expect(finalItem(h, 'req-1')).toMatchObject({ status: 'resolved', resolution: 'use_other', serverValue: '服务端值X' })
        expect(finalItem(h, 'req-2').status).toBe('synced')
        expect(h.engine.getSnapshot().pendingItems).toHaveLength(0)
      },
    },
    {
      name: 'overwrite：强制覆盖重发——同幂等 id、overwrite=true，重发成功转 synced',
      run: async (h) => {
        await buildConflictHead(h, '服务端值X')
        h.sender.respondWith({ kind: 'success' })

        h.engine.resolveConflict('req-1', 'overwrite')
        await h.engine.flushPending()

        expect(finalItem(h, 'req-1')).toMatchObject({ status: 'synced', attempts: 2 })
        expect(h.sent()[1].overwrite).toBe(true)
        expect(h.sent()[1].item.clientRequestId).toBe('req-1')
        expect(h.sent()[1].item.localValue).toBe('1')
        expect(h.engine.getSnapshot().pendingItems).toHaveLength(0)
      },
    },
    {
      name: 'copy：另存为副本——原项标记冲突已处理（resolution=copy），原始内容保留可取回',
      run: async (h) => {
        await buildConflictHead(h, '服务端值X')
        h.sender.respondWith({ kind: 'success' })

        h.engine.resolveConflict('req-1', 'copy')
        await h.engine.flushPending()

        expect(finalItem(h, 'req-1')).toMatchObject({ status: 'resolved', resolution: 'copy' })
        expect(h.engine.getItemContent('req-1')).toMatchObject({
          clientRequestId: 'req-1',
          snapshot: 'before',
          localValue: '1',
        })
        expect(finalItem(h, 'req-2').status).toBe('synced')
      },
    },
    {
      name: '失败项内容可复制不丢失：failed 保留编辑前快照 + 本地值，getItemContent 可取回',
      run: async (h) => {
        h.sender.respondWith({ kind: 'error', message: '500' })
        enqueueOnly(h, '1')
        await h.engine.flushPending()

        expect(finalItem(h, 'req-1').status).toBe('failed')
        expect(h.engine.getItemContent('req-1')).toStrictEqual({
          clientRequestId: 'req-1',
          target: cellTarget('r1', 'f_name', '1'),
          snapshot: 'before',
          localValue: '1',
          status: 'failed',
        })
        expect(h.engine.getItemContent('req-unknown')).toBeNull()
      },
    },
    {
      name: '裁决口只认冲突态：非冲突项/未知 id 调 resolveConflict 明确抛错（不静默吞）',
      run: async (h) => {
        enqueueOnly(h, '1')
        await h.engine.flushPending()

        expect(() => h.engine.resolveConflict('req-1', 'use_other')).toThrow(/冲突/)
        expect(() => h.engine.resolveConflict('req-unknown', 'copy')).toThrow(/冲突/)
      },
    },
    {
      name: '重放会话中冲突：phase 保持 replaying、进度冻结；裁决后续跑至全部完成',
      run: async (h) => {
        h.sender.respondWith({ kind: 'conflict', serverValue: '服务端值X' })
        enqueueOnly(h, '1')
        enqueueOnly(h, '2')
        h.engine.goOffline()
        h.sender.respondWith({ kind: 'success' })

        const progresses: Array<unknown> = []
        h.engine.subscribe(() => progresses.push(h.engine.getSnapshot().replayProgress))
        await h.engine.reconnect() // 第 1 条冲突，会话暂停
        expect(h.engine.getSnapshot().phase).toBe('replaying')
        expect(finalItem(h, 'req-1').status).toBe('conflict')
        expect(finalItem(h, 'req-2').attempts).toBe(0)

        h.engine.resolveConflict('req-1', 'use_other')
        await h.engine.flushPending()

        expect(finalItem(h, 'req-1')).toMatchObject({ status: 'resolved', resolution: 'use_other' })
        expect(finalItem(h, 'req-2').status).toBe('synced')
        expect(h.engine.getSnapshot().phase).toBe('idle')
        expect(progresses).toContainEqual({ done: 1, total: 2 }) // 裁决结算计入进度
      },
    },
  ]

  it.each(CASES)('$name', async ({ run }) => {
    const h = createEngine({ onReconnectSync: () => {} })
    await run(h)
  })
})

// ---------- 语义区 5：队首滞留 >5 分钟触发「未同步清单 + 复制入口」提示流 ----------

describe('core/offline 状态迁移（表驱动）：滞留提示', () => {
  interface StallCase {
    name: string
    run: (h: Harness) => Promise<void>
  }

  /** 收集滞留通知（回调 + 订阅双通道，UI 消费是后续票/M3 的事）。 */
  function collectStalls(h: Harness): { notifications: StallNotification[] } {
    const notifications: StallNotification[] = []
    h.engine.addStallListener((notification) => notifications.push(notification))
    return { notifications }
  }

  const CASES: StallCase[] = [
    {
      name: '队首滞留 >5 分钟触发提示流：恰好阈值前不触发，越线即触发；同一队首只提示一次',
      run: async (h) => {
        const { notifications } = collectStalls(h)
        h.sender.holdNext()
        enqueueOnly(h, '1')

        h.clock.advance(STALL_THRESHOLD_MS - 1)
        expect(notifications).toHaveLength(0)
        h.clock.advance(1)

        expect(notifications).toHaveLength(1)
        expect(notifications[0].head.clientRequestId).toBe('req-1')
        expect(notifications[0].stalledMs).toBe(STALL_THRESHOLD_MS)
        h.clock.advance(STALL_THRESHOLD_MS * 2)
        expect(notifications).toHaveLength(1) // 同队首 episode 不重复提示
      },
    },
    {
      name: '提示流载荷 = 未同步清单（入队顺序、全部未结算项）+ 复制入口内容可经 getItemContent 取回',
      run: async (h) => {
        const { notifications } = collectStalls(h)
        h.sender.holdNext()
        enqueueOnly(h, '1')
        enqueueOnly(h, '2')

        h.clock.advance(STALL_THRESHOLD_MS + 1)

        expect(notifications).toHaveLength(1)
        expect(notifications[0].unsyncedItems.map((item) => item.localValue)).toStrictEqual(['1', '2'])
        const copied = h.engine.getItemContent(notifications[0].unsyncedItems[1].clientRequestId)
        expect(copied).toMatchObject({ localValue: '2', snapshot: 'before', status: 'queued' })
      },
    },
    {
      name: '队首结算后新队首重新武装：新 episode 独立计时并再次提示',
      run: async (h) => {
        const { notifications } = collectStalls(h)
        h.sender.holdNext()
        enqueueOnly(h, '1')
        enqueueOnly(h, '2')

        h.clock.advance(STALL_THRESHOLD_MS) // 第 1 次：队首 req-1
        expect(notifications.map((n) => n.head.clientRequestId)).toStrictEqual(['req-1'])

        h.sender.settleNext({ kind: 'success' })
        await untilInFlight(h.sender) // req-2 进入在途，队首更换
        h.clock.advance(1) // req-2 的滞留线已过（同批入队），新 episode 立即触发

        expect(notifications.map((n) => n.head.clientRequestId)).toStrictEqual(['req-1', 'req-2'])
        h.sender.settleNext({ kind: 'success' })
        await h.engine.flushPending()
        h.clock.advance(STALL_THRESHOLD_MS * 3)
        expect(notifications).toHaveLength(2) // 全部完成后不再触发
      },
    },
    {
      name: '全部完成后不触发：队列清空后推进远超阈值的时间无提示',
      run: async (h) => {
        const { notifications } = collectStalls(h)
        enqueueAndSettle(h, '1')
        await enqueueAndSettle(h, '2')

        h.clock.advance(STALL_THRESHOLD_MS * 3)
        expect(notifications).toHaveLength(0)
      },
    },
    {
      name: '冲突队首同样计入滞留（未结算即滞留）：conflict 队首越线触发',
      run: async (h) => {
        const { notifications } = collectStalls(h)
        h.sender.respondWith({ kind: 'conflict', serverValue: '服务端值X' })
        enqueueOnly(h, '1')
        await h.engine.flushPending()

        h.clock.advance(STALL_THRESHOLD_MS + 1)
        expect(notifications).toHaveLength(1)
        expect(notifications[0].head.status).toBe('conflict')
        expect(notifications[0].unsyncedItems).toHaveLength(1)
      },
    },
    {
      name: '滞留阈值可注入（缺省 STALL_THRESHOLD_MS=5 分钟）',
      run: async () => {
        const clock = new ManualClock()
        const notifications: StallNotification[] = []
        const engine = new OfflineQueueEngine({
          sender: () => new Promise<SendResult>(() => {}), // 恒在途
          scheduler: clock,
          generateRequestId: () => 'req-1',
          stallThresholdMs: 100,
          onStall: (notification) => notifications.push(notification), // onStall 选项通道
        })
        expect(STALL_THRESHOLD_MS).toBe(5 * 60 * 1000)

        engine.enqueue({ target: cellTarget('r1', 'f_a', '1'), snapshot: null, localValue: '1' })
        clock.advance(99)
        expect(notifications).toHaveLength(0)
        clock.advance(1)
        expect(notifications).toHaveLength(1)
        expect(notifications[0].head.clientRequestId).toBe('req-1')
      },
    },
  ]

  it.each(CASES)('$name', async ({ run }) => {
    const h = createEngine({ onReconnectSync: () => {} })
    await run(h)
  })
})
