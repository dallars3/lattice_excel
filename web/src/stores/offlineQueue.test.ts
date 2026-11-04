// offlineQueueStore zustand 接线测试（票 06，技术方案 5.3/第 8 章）。
// 消费方为 banner/grid；多标签隔离语义经「两个 store 实例互不可见对方队列」用例锁死
// （技术方案 5.3：多标签页各持独立队列与会话）。
import { describe, expect, it } from 'vitest'

import { ManualClock } from '@/test/manual-clock'

import type { OfflineQueueTarget, SendResult } from '@/core/offline'

import { createOfflineQueueStore, offlineQueueStore, type OfflineQueueState } from './offlineQueue'

// ---------- Fake sender（与 core 侧同构的最小录制器） ----------

class FakeSender {
  readonly calls: Array<{ clientRequestId: string; localValue: unknown; overwrite: boolean }> = []
  private script: SendResult[] = []
  private hold = false
  private readonly held: Array<(result: SendResult) => void> = []

  respondWith(...results: SendResult[]): this {
    this.script.push(...results)
    return this
  }

  holdNext(): this {
    this.hold = true
    return this
  }

  resumeAuto(): this {
    this.hold = false
    return this
  }

  send = async (item: { clientRequestId: string; localValue: unknown }, options: { overwrite: boolean }): Promise<SendResult> => {
    this.calls.push({ clientRequestId: item.clientRequestId, localValue: item.localValue, overwrite: options.overwrite })
    if (this.hold) {
      return new Promise<SendResult>((resolve) => this.held.push(resolve))
    }
    return this.script.shift() ?? { kind: 'success' }
  }

  settleNext(result: SendResult): void {
    const resolve = this.held.shift()
    if (resolve === undefined) {
      throw new Error('FakeSender：没有在途发送可回填')
    }
    resolve(result)
  }

  pendingCount(): number {
    return this.held.length
  }
}

function cellTarget(value: string): OfflineQueueTarget {
  return { kind: 'update_cell', url: '/bases/b1/tables/t1/records/r1/cells/f_name', body: { value } }
}

interface Harness {
  store: ReturnType<typeof createOfflineQueueStore>
  clock: ManualClock
  sender: FakeSender
}

function createStore(options?: { respond?: SendResult[] }): Harness {
  const clock = new ManualClock()
  const sender = new FakeSender()
  if (options?.respond !== undefined) {
    sender.respondWith(...options.respond)
  }
  const store = createOfflineQueueStore({
    sender: sender.send,
    scheduler: clock,
    generateRequestId: (() => {
      let seq = 0
      return () => `req-${(seq += 1)}`
    })(),
  })
  return { store, clock, sender }
}

function enqueue(store: ReturnType<typeof createOfflineQueueStore>, localValue: string): void {
  const outcome = store.getState().enqueue({ target: cellTarget(localValue), snapshot: 'before', localValue })
  if (!outcome.accepted) {
    throw new Error('在线入队应被接受')
  }
}

// ---------- 实例隔离（多标签语义） ----------

describe('offlineQueueStore 工厂：多标签实例隔离（票 06 AC）', () => {
  it('两个 store 实例互不可见对方队列：A 入队只进 A 的队列，B 的 sender 零调用', async () => {
    const a = createStore()
    const b = createStore()

    enqueue(a.store, 'A 编辑')
    await a.store.getState().flushPending()

    expect(a.store.getState().items).toHaveLength(1)
    expect(a.store.getState().items[0].localValue).toBe('A 编辑')
    expect(a.sender.calls).toHaveLength(1)
    expect(b.store.getState().items).toHaveLength(0)
    expect(b.store.getState().pendingItems).toHaveLength(0)
    expect(b.sender.calls).toHaveLength(0)
  })

  it('相位互不影响：B 断线不改变 A 的相位；A 的裁决不触碰 B 的队列', async () => {
    const a = createStore()
    const b = createStore()

    b.store.getState().goOffline()
    expect(b.store.getState().phase).toBe('offline')
    expect(a.store.getState().phase).toBe('idle')

    enqueue(a.store, 'A 编辑')
    expect(b.store.getState().items).toHaveLength(0) // B offline 只拒 B 自己的新编辑
  })

  it('singleton offlineQueueStore 与工厂实例互不影响（单例仅供 banner/grid 页内消费）', () => {
    const a = createStore()

    offlineQueueStore.getState().goOffline()
    const outcome = offlineQueueStore.getState().enqueue({ target: cellTarget('X'), localValue: 'X' })

    expect(outcome).toStrictEqual({ accepted: false, reason: 'offline_readonly' })
    expect(offlineQueueStore.getState().phase).toBe('offline')
    expect(a.store.getState().phase).toBe('idle')
    expect(a.store.getState().items).toHaveLength(0)
  })
})

// ---------- 状态镜像与动作委托 ----------

describe('offlineQueueStore 工厂：状态可订阅 + 动作委托', () => {
  it('状态镜像：enqueue → sender 即发 → 结算 synced 全程反映在 zustand state 上', async () => {
    const { store } = createStore()

    const phases: string[] = []
    store.subscribe((state) => phases.push(state.phase))
    enqueue(store, 'B')
    await store.getState().flushPending()

    expect(store.getState().phase).toBe('idle')
    expect(store.getState().items).toHaveLength(1)
    expect(store.getState().items[0]).toMatchObject({ status: 'synced', localValue: 'B' })
    expect(store.getState().pendingItems).toHaveLength(0)
    expect(store.getState().replayProgress).toBeNull()
    expect(phases.length).toBeGreaterThan(0) // 状态镜像推送发生过
    expect(phases.every((phase) => phase === 'idle')).toBe(true) // 相位全程 idle（入队即发非重放）
  })

  it('断线只读 + 重放：goOffline 拒新编辑，reconnect 先补增量再顺序重放并清进度', async () => {
    const { store, sender } = createStore()
    sender.holdNext()
    enqueue(store, '1')
    enqueue(store, '2')
    sender.settleNext({ kind: 'error', message: '断线' }) // 第 1 条失败滞留
    await store.getState().flushPending()

    store.getState().goOffline()
    const rejected = store.getState().enqueue({ target: cellTarget('新编辑'), localValue: '新编辑' })
    expect(rejected).toStrictEqual({ accepted: false, reason: 'offline_readonly' })
    expect(store.getState().items).toHaveLength(2) // 已入队项保留

    sender.resumeAuto().respondWith({ kind: 'success' }, { kind: 'success' })
    await store.getState().reconnect()

    expect(store.getState().phase).toBe('idle')
    expect(store.getState().replayProgress).toBeNull()
    expect(store.getState().pendingItems).toHaveLength(0)
    expect(sender.calls.map((call) => call.localValue)).toStrictEqual(['1', '1', '2'])
  })

  it('冲突三动作委托：conflict 后 resolveConflict(use_other) 落账 resolved，内容可经 getItemContent 取回', async () => {
    const { store } = createStore({ respond: [{ kind: 'conflict', serverValue: '服务端值X' }] })
    enqueue(store, '1')
    await store.getState().flushPending()

    expect(store.getState().items[0]).toMatchObject({ status: 'conflict', serverValue: '服务端值X' })

    store.getState().resolveConflict(store.getState().items[0].clientRequestId, 'use_other')
    await store.getState().flushPending()

    expect(store.getState().items[0]).toMatchObject({ status: 'resolved', resolution: 'use_other' })
    const content = store.getState().getItemContent(store.getState().items[0].clientRequestId)
    expect(content).toMatchObject({ localValue: '1', snapshot: 'before' })
  })

  it('滞留提示流经 store 可消费（onStall 注入通道）', () => {
    const clock = new ManualClock()
    const notifications: string[] = []
    const store = createOfflineQueueStore({
      sender: () => new Promise<SendResult>(() => {}), // 恒在途
      scheduler: clock,
      generateRequestId: () => 'req-1',
      stallThresholdMs: 100,
      onStall: (notification) => notifications.push(notification.head.clientRequestId),
    })
    enqueue(store, '1')

    clock.advance(101)
    expect(notifications).toStrictEqual(['req-1'])
    expect(store.getState().pendingItems).toHaveLength(1)
  })

  it('未注入生成器时默认复用票 04 createRequestId（uuid v4 口径一致）', () => {
    const store = createOfflineQueueStore({ sender: () => Promise.resolve({ kind: 'error' }) })
    const outcome = store.getState().enqueue({ target: cellTarget('X'), localValue: 'X' })

    if (!outcome.accepted) {
      throw new Error('在线入队应被接受')
    }
    expect(outcome.item.clientRequestId).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i,
    )
  })

  it('state 满足技术方案第 8 章口径：待传队列 + 重放进度 + 相位（banner/grid 消费面）', () => {
    const { store } = createStore()
    const state: OfflineQueueState = store.getState()
    expect(state).toMatchObject({ phase: 'idle', items: [], pendingItems: [], replayProgress: null })
  })
})
