// FakeTransport（票 05 测试接缝）：脚本化注入服务端行为——可控 open/消息/断线/拒绝连接，
// 并记录客户端发出的原始帧供对账断言。全部测试经此注入，绝不触真实网络。
import { parseClientMessage, type ClientMessage, type ServerMessage } from './messages'
import type { Transport, TransportCloseInfo, TransportHandlers } from './transport'

export class FakeTransport implements Transport {
  /** 客户端发出的原始帧（按发出顺序）。 */
  readonly sent: string[] = []
  connectCount = 0
  closeCount = 0
  private readonly handlers: TransportHandlers
  private openState = false
  private closeNotified = false

  constructor(handlers: TransportHandlers) {
    this.handlers = handlers
  }

  connect(): void {
    this.connectCount += 1
  }

  send(raw: string): void {
    if (!this.openState) {
      throw new Error('FakeTransport.send：连接未建立（真实 WS 同样会抛错）')
    }
    this.sent.push(raw)
  }

  /** 客户端主动关闭：通知一次 onClose（真实 WS 的 close 事件语义）。 */
  close(): void {
    this.closeCount += 1
    if (this.closeNotified) {
      return
    }
    this.closeNotified = true
    this.openState = false
    this.handlers.onClose({})
  }

  // ---------- 以下为测试脚本化注入面（模拟服务端行为） ----------

  isOpen(): boolean {
    return this.openState
  }

  serverOpen(): void {
    this.openState = true
    this.closeNotified = false // 新连接开始新的生命周期
    this.handlers.onOpen()
  }

  /** 注入服务端帧：可传原始字符串或类型化消息对象（自动序列化）。 */
  serverMessage(raw: string | ServerMessage): void {
    this.handlers.onMessage(typeof raw === 'string' ? raw : JSON.stringify(raw))
  }

  /** 服务端断开已建立的连接。 */
  serverClose(info?: TransportCloseInfo): void {
    this.openState = false
    this.closeNotified = true
    this.handlers.onClose(info ?? {})
  }

  /** 连接被拒绝：connect 后未 open 即失败（首次连接失败/重连失败都用它）。 */
  failConnect(info?: TransportCloseInfo): void {
    this.openState = false
    this.closeNotified = true
    this.handlers.onClose(info ?? { reason: 'refused' })
  }

  /** 发出的原始帧解码为类型化客户端消息（过滤非法帧），断言用。 */
  sentMessages(): ClientMessage[] {
    const decoded: ClientMessage[] = []
    for (const raw of this.sent) {
      const message = parseClientMessage(raw)
      if (message !== null) {
        decoded.push(message)
      }
    }
    return decoded
  }
}

export interface FakeTransportHarness {
  factory: (handlers: TransportHandlers) => FakeTransport
  /** 按创建顺序记录全部 Fake 实例（覆盖重连产生多个实例的场景）。 */
  transports: FakeTransport[]
}

/** 测试辅助：工厂注入面 + 实例追溯。 */
export function createFakeTransportFactory(): FakeTransportHarness {
  const transports: FakeTransport[] = []
  return {
    factory: (handlers) => {
      const transport = new FakeTransport(handlers)
      transports.push(transport)
      return transport
    },
    transports,
  }
}
