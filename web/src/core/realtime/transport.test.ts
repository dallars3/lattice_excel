import { describe, expect, it, vi } from 'vitest'

import { FakeTransport, createFakeTransportFactory } from './fake-transport'
import { WsTransport, type TransportHandlers } from './transport'

// 脚本化服务端事件注入的最小处理器集（记录调用供断言）。
function recordingHandlers() {
  const events: string[] = []
  const raws: string[] = []
  const handlers: TransportHandlers = {
    onOpen: () => events.push('open'),
    onMessage: (raw) => {
      events.push('message')
      raws.push(raw)
    },
    onClose: (info) => events.push(`close:${info.code ?? ''}:${info.reason ?? ''}`),
  }
  return { events, raws, handlers }
}

describe('core/realtime FakeTransport（脚本化注入，无真实网络）', () => {
  it('脚本化 open→message→close 按序驱动处理器', () => {
    const { events, raws, handlers } = recordingHandlers()
    const transport = new FakeTransport(handlers)

    transport.serverOpen()
    transport.serverMessage('{"t":"snapshot","seq":1,"member_list":[]}')
    transport.serverClose({ code: 1006, reason: 'abnormal' })

    expect(events).toEqual(['open', 'message', 'close:1006:abnormal'])
    expect(raws).toEqual(['{"t":"snapshot","seq":1,"member_list":[]}'])
    expect(transport.isOpen()).toBe(false)
  })

  it('客户端 send 记录原始帧并可通过 sentMessages 解码为类型化消息；未 open 时 send 抛错', () => {
    const { handlers } = recordingHandlers()
    const transport = new FakeTransport(handlers)

    expect(() => transport.send('{"t":"join"}')).toThrow()
    transport.serverOpen()
    transport.send('{"t":"join","base_id":"b1","table_id":"t1"}')
    transport.send('{"t":"heartbeat"}')

    expect(transport.sent).toEqual(['{"t":"join","base_id":"b1","table_id":"t1"}', '{"t":"heartbeat"}'])
    expect(transport.sentMessages()).toEqual([
      { t: 'join', base_id: 'b1', table_id: 't1' },
      { t: 'heartbeat' },
    ])
  })

  it('connect/close 计数与 failConnect（未 open 即拒绝）可脚本化', () => {
    const { events, handlers } = recordingHandlers()
    const transport = new FakeTransport(handlers)

    transport.connect()
    transport.failConnect({ code: 1006, reason: 'refused' })
    expect(transport.connectCount).toBe(1)
    expect(events).toEqual(['close:1006:refused'])
    expect(transport.isOpen()).toBe(false)

    transport.serverOpen()
    transport.close()
    expect(transport.closeCount).toBe(1)
    expect(events).toEqual(['close:1006:refused', 'open', 'close::'])
  })

  it('close 幂等：仅首个 close 通知 onClose', () => {
    const { events, handlers } = recordingHandlers()
    const transport = new FakeTransport(handlers)
    transport.serverOpen()
    transport.close()
    transport.close()
    expect(events.filter((e) => e.startsWith('close'))).toHaveLength(1)
    expect(transport.closeCount).toBe(2)
  })

  it('createFakeTransportFactory：每次连接产出独立 Fake 且全部可追溯（重连多实例场景）', () => {
    const { factory, transports } = createFakeTransportFactory()
    const t1 = factory({ onOpen: () => {}, onMessage: () => {}, onClose: () => {} })
    const t2 = factory({ onOpen: () => {}, onMessage: () => {}, onClose: () => {} })

    expect(transports).toEqual([t1, t2])
    expect(t1).not.toBe(t2)
    t1.serverOpen()
    expect(t2.isOpen()).toBe(false)
  })
})

describe('core/realtime WsTransport（生产占位，stub WebSocket，无真实网络）', () => {
  // 最小 WebSocket 桩：只验证 WsTransport 的接线与转发，不触网络。
  class StubWebSocket {
    static OPEN = 1
    static instances: StubWebSocket[] = []
    onopen: (() => void) | null = null
    onmessage: ((ev: { data: unknown }) => void) | null = null
    onclose: ((ev: { code: number; reason: string }) => void) | null = null
    onerror: (() => void) | null = null
    readyState = 0
    readonly sent: string[] = []
    closed = false

    constructor(readonly url: string) {
      StubWebSocket.instances.push(this)
    }

    send(data: string): void {
      this.sent.push(data)
    }

    close(): void {
      this.closed = true
      this.readyState = 3
      this.onclose?.({ code: 1000, reason: '' })
    }
  }

  const stubGlobalWebSocket = () => {
    vi.stubGlobal('WebSocket', StubWebSocket as unknown as typeof WebSocket)
    StubWebSocket.instances = []
    return () => vi.unstubAllGlobals()
  }

  it('可构造（不触网络），close 在未连接时安全', () => {
    const restore = stubGlobalWebSocket()
    const { handlers } = recordingHandlers()
    const transport = new WsTransport(handlers, { url: 'ws://localhost:0/m1-placeholder' })
    expect(() => transport.close()).not.toThrow()
    expect(StubWebSocket.instances).toEqual([])
    restore()
  })

  it('connect 经全局 WebSocket 建立并接线 open/message/close 到处理器', () => {
    const restore = stubGlobalWebSocket()
    const { events, raws, handlers } = recordingHandlers()
    const transport = new WsTransport(handlers, { url: 'ws://localhost:0/m1-placeholder' })
    transport.connect()

    expect(StubWebSocket.instances).toHaveLength(1)
    const ws = StubWebSocket.instances[0]
    expect(ws.url).toBe('ws://localhost:0/m1-placeholder')

    ws.readyState = StubWebSocket.OPEN
    ws.onopen?.()
    ws.onmessage?.({ data: '{"t":"ack","msg_id":"m1"}' })
    ws.onclose?.({ code: 1006, reason: 'gone' })

    expect(events).toEqual(['open', 'message', 'close:1006:gone'])
    expect(raws).toEqual(['{"t":"ack","msg_id":"m1"}'])
    restore()
  })

  it('send 转发到 WebSocket；未 open（readyState 不为 OPEN）时抛错', () => {
    const restore = stubGlobalWebSocket()
    const { handlers } = recordingHandlers()
    const transport = new WsTransport(handlers, { url: 'ws://localhost:0/m1-placeholder' })
    transport.connect()
    const ws = StubWebSocket.instances[0]

    expect(() => transport.send('{"t":"leave"}')).toThrow()
    ws.readyState = StubWebSocket.OPEN
    transport.send('{"t":"leave"}')
    expect(ws.sent).toEqual(['{"t":"leave"}'])
    restore()
  })

  it('close 委托给 WebSocket.close', () => {
    const restore = stubGlobalWebSocket()
    const { handlers } = recordingHandlers()
    const transport = new WsTransport(handlers, { url: 'ws://localhost:0/m1-placeholder' })
    transport.connect()
    const ws = StubWebSocket.instances[0]
    transport.close()
    expect(ws.closed).toBe(true)
    restore()
  })
})
