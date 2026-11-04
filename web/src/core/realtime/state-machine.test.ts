import { describe, expect, it, vi } from 'vitest'

import { ManualClock } from '@/test/manual-clock'

import { createFakeTransportFactory } from './fake-transport'
import type { SnapshotMessage } from './messages'
import { BACKOFF_INITIAL_MS, BACKOFF_MAX_MS, RealtimeClient, type RealtimeClientOptions, type RoomSelector, type StateTransition } from './state-machine'

const ROOM: RoomSelector = { base_id: 'b1', table_id: 't1' }

// 期望值独立事实源：《接口文档-v0.6.md》8.6.8 示例与 8.2 载荷字段。
const SNAPSHOT: SnapshotMessage = {
  t: 'snapshot',
  seq: 1203,
  member_list: [{ user_id: 'usr_1', color: 'blue', online: true, internal: true }],
}

const REVOKED = { t: 'session_revoked', reason: 'link_closed' } as const

interface Harness {
  client: RealtimeClient
  clock: ManualClock
  transports: ReturnType<typeof createFakeTransportFactory>['transports']
}

function createClient(options?: Partial<RealtimeClientOptions>): Harness {
  const clock = new ManualClock()
  const { factory, transports } = createFakeTransportFactory()
  const client = new RealtimeClient({ transportFactory: factory, scheduler: clock, ...options })
  return { client, clock, transports }
}

/** 造出指定前置态（各前置态构造方式见用例表）。 */
function buildState(h: Harness, from: string): void {
  switch (from) {
    case 'idle':
      return
    case 'connecting':
      h.client.connect(ROOM)
      return
    case 'joined':
      h.client.connect(ROOM)
      h.transports[0].serverOpen()
      h.transports[0].serverMessage(SNAPSHOT)
      return
    case 'reconnecting':
      buildState(h, 'joined')
      h.transports[0].serverClose({ code: 1006, reason: 'abnormal' })
      return
    case 'revoked':
      buildState(h, 'joined')
      h.transports[0].serverMessage(REVOKED)
      return
    default:
      throw new Error(`未知前置态 ${from}`)
  }
}

describe('core/realtime 五态迁移（表驱动）', () => {
  interface TransitionCase {
    from: string
    trigger: string
    act: (h: Harness) => void
    to: string
  }

  const CASES: TransitionCase[] = [
    {
      from: 'idle',
      trigger: 'connect()',
      act: (h) => h.client.connect(ROOM),
      to: 'connecting',
    },
    {
      from: 'connecting',
      trigger: 'open + W-08 snapshot',
      act: (h) => {
        h.transports[0].serverOpen()
        h.transports[0].serverMessage(SNAPSHOT)
      },
      to: 'joined',
    },
    {
      from: 'connecting',
      trigger: '连接被拒绝（未 open 即 close）',
      act: (h) => h.transports[0].failConnect({ code: 1006 }),
      to: 'reconnecting',
    },
    {
      from: 'joined',
      trigger: 'transport 断线',
      act: (h) => h.transports[0].serverClose({ code: 1006 }),
      to: 'reconnecting',
    },
    {
      from: 'reconnecting',
      trigger: '退避到期重连成功（open + snapshot）',
      act: (h) => {
        h.clock.advance(BACKOFF_INITIAL_MS)
        h.transports[1].serverOpen()
        h.transports[1].serverMessage(SNAPSHOT)
      },
      to: 'joined',
    },
    {
      from: 'reconnecting',
      trigger: '重连再次失败',
      act: (h) => {
        h.clock.advance(BACKOFF_INITIAL_MS)
        h.transports[1].failConnect({ code: 1006 })
      },
      to: 'reconnecting',
    },
    {
      from: 'joined',
      trigger: 'W-15 session_revoked',
      act: (h) => h.transports[0].serverMessage(REVOKED),
      to: 'revoked',
    },
    {
      from: 'reconnecting',
      trigger: '重连中收到 W-15 session_revoked',
      act: (h) => {
        h.clock.advance(BACKOFF_INITIAL_MS)
        h.transports[1].serverOpen()
        h.transports[1].serverMessage(REVOKED)
      },
      to: 'revoked',
    },
    {
      from: 'revoked',
      trigger: '任何事件（snapshot/open/close/re-connect）不再迁出',
      act: (h) => {
        h.transports[0].serverMessage(SNAPSHOT)
        h.transports[0].serverOpen()
        h.transports[0].serverClose()
        h.client.connect(ROOM)
      },
      to: 'revoked',
    },
  ]

  it.each(CASES)('$from --$trigger→ $to', ({ from, act, to }) => {
    const h = createClient()
    buildState(h, from)
    expect(h.client.getState()).toBe(from)
    act(h)
    expect(h.client.getState()).toBe(to)
  })

  it('五态全集恰为 idle/connecting/joined/reconnecting/revoked（技术方案 5.2）', () => {
    expect(new Set(CASES.flatMap((c) => [c.from, c.to]))).toEqual(
      new Set(['idle', 'connecting', 'joined', 'reconnecting', 'revoked']),
    )
  })
})

describe('core/realtime 断线退避（指数 1s 起、上限 30s，注入时钟）', () => {
  it('退避从 1s 起：断线后 999ms 无动作，1000ms 触发重连', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverClose()
    h.clock.advance(999)
    expect(h.transports).toHaveLength(1)
    h.clock.advance(1)
    expect(h.transports).toHaveLength(2)
  })

  it('连续失败按 1,2,4,8,16,30,30s 指数退避并在 30s 封顶', () => {
    const h = createClient()
    h.client.connect(ROOM)
    const delays = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000]
    for (let i = 0; i < delays.length; i += 1) {
      h.transports[h.transports.length - 1].failConnect({ code: 1006 })
      h.clock.advance(delays[i])
      expect(h.transports, `第 ${i + 1} 次退避 ${delays[i]}ms`).toHaveLength(i + 2)
    }
    expect(BACKOFF_INITIAL_MS).toBe(1_000)
    expect(BACKOFF_MAX_MS).toBe(30_000)
  })

  it('重连成功回 joined 后再次断线，退避重置回 1s', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverClose()
    h.clock.advance(BACKOFF_INITIAL_MS)
    h.transports[1].serverOpen()
    h.transports[1].serverMessage(SNAPSHOT)
    expect(h.client.getState()).toBe('joined')

    h.transports[1].serverClose()
    h.clock.advance(999)
    expect(h.transports).toHaveLength(2)
    h.clock.advance(1)
    expect(h.transports).toHaveLength(3)
  })

  it('revoked 清除待触发的重连定时器（无残留定时任务）', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverClose()
    expect(h.clock.pendingCount()).toBe(1)
    h.transports[0].serverMessage(REVOKED)
    expect(h.clock.pendingCount()).toBe(0)
    h.clock.advance(120_000)
    expect(h.transports).toHaveLength(1)
  })
})

describe('core/realtime 重连对账（join/replay_request 携 last_seq）', () => {
  it('首次接入：join 不带 last_seq，snapshot 后本地高水位 = seq', () => {
    const h = createClient()
    h.client.connect(ROOM)
    h.transports[0].serverOpen()
    expect(h.transports[0].sentMessages()).toEqual([{ t: 'join', base_id: 'b1', table_id: 't1' }])
    h.transports[0].serverMessage(SNAPSHOT)
    expect(h.client.getLastSeq()).toBe(1203)
    expect(h.client.getState()).toBe('joined')
  })

  it('重连成功回 joined：join 与 replay_request 均带 last_seq=本地高水位', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverMessage({ t: 'cell_changed', seq: 1204, record_id: 'rec_1', field_id: 'fld_num', value: 126, version: 8, operator: 'usr_1' })
    h.transports[0].serverClose({ code: 1006 })

    h.clock.advance(BACKOFF_INITIAL_MS)
    h.transports[1].serverOpen()
    expect(h.transports[1].sentMessages()[0]).toEqual({ t: 'join', base_id: 'b1', table_id: 't1', last_seq: 1204 })
    h.transports[1].serverMessage({ t: 'snapshot', seq: 1210, member_list: [] })
    expect(h.client.getState()).toBe('joined')
    expect(h.transports[1].sentMessages()[1]).toEqual({ t: 'replay_request', last_seq: 1204 })
    expect(h.client.getLastSeq()).toBe(1210)
  })

  it('高水位单调取 max：seq 回退不降低本地高水位', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverMessage({ t: 'cell_changed', seq: 1209, record_id: 'rec_1', field_id: 'fld_num', value: 1, version: 9, operator: 'usr_1' })
    h.transports[0].serverMessage({ t: 'record_deleted', seq: 1205, record_id: 'rec_1' })
    expect(h.client.getLastSeq()).toBe(1209)
  })

  it('首次接入（无历史高水位）重连成功后不发 replay_request', () => {
    const h = createClient()
    h.client.connect(ROOM)
    h.transports[0].failConnect({ code: 1006 })
    h.clock.advance(BACKOFF_INITIAL_MS)
    h.transports[1].serverOpen()
    // 无历史时 join 也不带 last_seq（本地从未收到 snapshot，无高水位）。
    expect(h.transports[1].sentMessages()).toEqual([{ t: 'join', base_id: 'b1', table_id: 't1' }])
    h.transports[1].serverMessage(SNAPSHOT)
    expect(h.client.getState()).toBe('joined')
    expect(h.transports[1].sentMessages()).toHaveLength(1) // 仅 join，无 replay_request
    expect(h.client.getLastSeq()).toBe(1203)
  })
})

describe('core/realtime 心跳集成（15s，注入时钟）', () => {
  it('joined 态推进 15s 发 heartbeat；ack 应答后下个周期照常', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.clock.advance(15_000)
    expect(h.transports[0].sentMessages()).toEqual([
      { t: 'join', base_id: 'b1', table_id: 't1' },
      { t: 'heartbeat' },
    ])
    h.transports[0].serverMessage({ t: 'ack', msg_id: 'm1' })
    h.clock.advance(15_000)
    expect(h.transports[0].sentMessages()[2]).toEqual({ t: 'heartbeat' })
  })

  it('一个完整周期无应答判死：转 reconnecting，30s 内不再有 heartbeat 发出', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.clock.advance(15_000)
    h.clock.advance(15_000)
    expect(h.client.getState()).toBe('reconnecting')
    h.clock.advance(30_000)
    expect(h.transports[0].sentMessages().filter((m) => m.t === 'heartbeat')).toHaveLength(1)
  })

  it('reconnecting 态心跳停止：断线推进 30s 无 heartbeat', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverClose()
    h.clock.advance(30_000)
    for (const t of h.transports) {
      expect(t.sentMessages().some((m) => m.t === 'heartbeat')).toBe(false)
    }
  })

  it('心跳超时后按退避重连', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.clock.advance(15_000)
    h.clock.advance(15_000)
    expect(h.client.getState()).toBe('reconnecting')
    h.clock.advance(BACKOFF_INITIAL_MS)
    expect(h.transports).toHaveLength(2)
  })
})

describe('core/realtime session_revoked 不可逆（终态）', () => {
  it('revoked 后：connect 不再建新传输、消息/开闭事件全部短路、无任何帧发出', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverMessage(REVOKED)
    expect(h.client.getState()).toBe('revoked')
    const sentBefore = h.transports[0].sent.length

    h.client.connect(ROOM)
    expect(h.transports).toHaveLength(1)
    h.transports[0].serverOpen()
    h.transports[0].serverMessage(SNAPSHOT)
    h.transports[0].serverClose()
    h.client.disconnect()
    h.clock.advance(120_000)

    expect(h.client.getState()).toBe('revoked')
    expect(h.transports).toHaveLength(1)
    expect(h.transports[0].sent).toHaveLength(sentBefore)
    expect(h.clock.pendingCount()).toBe(0)
  })

  it('reconnecting 中收到 revoked：不可逆，退避定时器被清除', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.transports[0].serverClose()
    h.transports[0].serverMessage(REVOKED)
    expect(h.client.getState()).toBe('revoked')
    expect(h.clock.pendingCount()).toBe(0)
    h.clock.advance(120_000)
    expect(h.transports).toHaveLength(1)
  })
})

describe('core/realtime 事件分发（类型化可订阅）', () => {
  it('onMessage 泛型监听按序收到全部类型化服务端消息', () => {
    const h = createClient()
    const seen: string[] = []
    h.client.onMessage((msg) => seen.push(msg.t))
    buildState(h, 'joined')
    h.transports[0].serverMessage({ t: 'cell_changed', seq: 1204, record_id: 'rec_1', field_id: 'fld_num', value: 1, version: 8, operator: 'usr_1' })
    h.transports[0].serverMessage({ t: 'presence', user_id: 'usr_2', color: 'green', online: true, internal: false })
    expect(seen).toEqual(['snapshot', 'cell_changed', 'presence'])
  })

  it('onMessage(type) 按名分发且消息已类型化；off 后不再收到', () => {
    const h = createClient()
    buildState(h, 'joined')
    const onCell = vi.fn()
    const unsubscribe = h.client.onMessage('cell_changed', onCell)
    h.transports[0].serverMessage({ t: 'cell_changed', seq: 1204, record_id: 'rec_1', field_id: 'fld_num', value: 126, version: 8, operator: 'usr_1' })
    expect(onCell).toHaveBeenCalledTimes(1)
    expect(onCell.mock.calls[0][0]).toMatchObject({ t: 'cell_changed', seq: 1204, version: 8 })

    unsubscribe()
    h.transports[0].serverMessage({ t: 'cell_changed', seq: 1205, record_id: 'rec_1', field_id: 'fld_num', value: 2, version: 9, operator: 'usr_1' })
    expect(onCell).toHaveBeenCalledTimes(1)
  })

  it('状态迁移可订阅：监听器收到 from/to/atMs 序列', () => {
    const h = createClient()
    const transitions: StateTransition[] = []
    h.client.onStateChange((t) => transitions.push(t))
    h.client.connect(ROOM)
    h.transports[0].serverOpen()
    h.transports[0].serverMessage(SNAPSHOT)
    h.clock.advance(500)
    h.transports[0].serverClose()

    expect(transitions).toEqual([
      { from: 'idle', to: 'connecting', atMs: 0 },
      { from: 'connecting', to: 'joined', atMs: 0 },
      { from: 'joined', to: 'reconnecting', atMs: 500 },
    ])
  })

  it('非法帧不分发、不影响状态', () => {
    const h = createClient()
    buildState(h, 'joined')
    const seen: string[] = []
    h.client.onMessage((msg) => seen.push(msg.t))
    h.transports[0].serverMessage('not-json')
    h.transports[0].serverMessage('{"t":"unknown_msg"}')
    expect(seen).toEqual([])
    expect(h.client.getState()).toBe('joined')
  })
})

describe('core/realtime send 门控（非 joined 不盲发业务消息）', () => {
  it('joined 态 send 编码并透传帧，返回 true', () => {
    const h = createClient()
    buildState(h, 'joined')
    expect(h.client.send({ t: 'cursor', record_id: 'rec_1', field_id: 'fld_1' })).toBe(true)
    expect(h.transports[0].sentMessages()).toEqual([
      { t: 'join', base_id: 'b1', table_id: 't1' },
      { t: 'cursor', record_id: 'rec_1', field_id: 'fld_1' },
    ])
  })

  it.each(['connecting', 'reconnecting', 'revoked'] as const)('%s 态 send 被拒：返回 false、不产生帧', (from) => {
    const h = createClient()
    buildState(h, from)
    const sentBefore = h.transports[0]?.sent.length ?? 0
    expect(h.client.send({ t: 'cursor', record_id: 'rec_1' })).toBe(false)
    expect(h.transports[0]?.sent.length ?? 0).toBe(sentBefore)
  })
})

describe('core/realtime disconnect 与 connect 守卫', () => {
  it('joined 态 disconnect：发 leave、回 idle、关闭传输、清定时器', () => {
    const h = createClient()
    buildState(h, 'joined')
    h.client.disconnect()
    expect(h.client.getState()).toBe('idle')
    expect(h.transports[0].sentMessages().at(-1)).toEqual({ t: 'leave' })
    expect(h.transports[0].closeCount).toBe(1)
    h.clock.advance(120_000)
    expect(h.transports).toHaveLength(1)
  })

  it('reconnecting 态 disconnect：直接回 idle 且不再重连', () => {
    const h = createClient()
    buildState(h, 'reconnecting')
    h.client.disconnect()
    expect(h.client.getState()).toBe('idle')
    h.clock.advance(120_000)
    expect(h.transports).toHaveLength(1)
  })

  it('idle 态 disconnect 幂等；非 idle 态 connect 抛错；revoked 态 connect 不抛但保持终态', () => {
    const h = createClient()
    expect(() => h.client.disconnect()).not.toThrow()

    h.client.connect(ROOM)
    expect(() => h.client.connect(ROOM)).toThrow()
    h.client.disconnect()

    buildState(h, 'revoked')
    expect(() => h.client.connect(ROOM)).not.toThrow()
    expect(h.client.getState()).toBe('revoked')
  })
})
