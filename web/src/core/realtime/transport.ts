// 传输层接缝（票 05）：状态机对传输实现无感知——M3 接真 WS 时只替换 factory 的实现，
// 状态机与消费方零改动（架构验收点）。生产占位 WsTransport 用浏览器 WebSocket，
// M1 不接真实 URL（env.WS_URL 在 M3 才进入装配），类型完整、可构造。
export interface TransportCloseInfo {
  readonly code?: number
  readonly reason?: string
}

/** 传输层向上回调（连接建立 / 收到原始帧 / 关闭或拒绝）。 */
export interface TransportHandlers {
  onOpen(): void
  onMessage(raw: string): void
  onClose(info: TransportCloseInfo): void
}

/** 传输层最小接口：connect 建立连接（可按次调用表示每次重连），send 发原始帧，close 主动断开。 */
export interface Transport {
  connect(): void
  send(raw: string): void
  close(): void
}

/** 状态机构造传输实例的工厂：每次（重）连接调用一次，产出一个独立传输实例。 */
export type TransportFactory = (handlers: TransportHandlers) => Transport

export interface WsTransportOptions {
  readonly url: string
  readonly protocols?: string | string[]
}

export class WsTransport implements Transport {
  private readonly handlers: TransportHandlers
  private readonly options: WsTransportOptions
  private ws: WebSocket | null = null

  constructor(handlers: TransportHandlers, options: WsTransportOptions) {
    this.handlers = handlers
    this.options = options
  }

  connect(): void {
    const ws = new WebSocket(this.options.url, this.options.protocols)
    this.ws = ws
    ws.onopen = () => this.handlers.onOpen()
    ws.onmessage = (event) => {
      if (typeof event.data === 'string') {
        this.handlers.onMessage(event.data)
      }
    }
    ws.onclose = (event) => {
      this.ws = null
      this.handlers.onClose({ code: event.code, reason: event.reason })
    }
    // error 事件后浏览器必然跟随 close 事件，这里不重复回调 onClose。
    ws.onerror = () => undefined
  }

  send(raw: string): void {
    if (this.ws === null || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('WsTransport.send：连接未就绪')
    }
    this.ws.send(raw)
  }

  close(): void {
    this.ws?.close()
    this.ws = null
  }
}
