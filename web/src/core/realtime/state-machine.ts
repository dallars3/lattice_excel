// realtimeClient 状态机（票 05，技术方案 5.2）：五态
//   idle → connecting → joined(snapshot) → reconnecting（指数退避 1s 起、上限 30s）→ revoked
// 要点：
// - 断线从 connecting/joined/reconnecting 均入 reconnecting；重连成功回 joined 并发
//   replay_request(last_seq)（last_seq=本地 seq 高水位，首次接入无历史则不发）。
// - session_revoked → revoked 终态不可逆：此后任何事件直接短路，不再迁出、不再发帧。
// - 心跳 15s 仅在 joined 态运行；ack 重置超时判定；超时视为断线。
// - 传输层经 TransportFactory 注入，状态机对传输实现无感知（M3 换真传输零改动）。
// - 定时器/时钟经 Scheduler 注入，测试无真实等待。
import { encodeClientMessage, parseServerMessage, type ClientMessage, type ServerMessage, type ServerMessageMap, type ServerMessageName } from './messages'
import { HeartbeatMonitor } from './heartbeat'
import type { Scheduler } from './scheduler'
import { realScheduler } from './scheduler'
import type { Transport, TransportCloseInfo, TransportFactory, TransportHandlers } from './transport'

/** 实时通道五态。 */
export const REALTIME_STATES = ['idle', 'connecting', 'joined', 'reconnecting', 'revoked'] as const
export type RealtimeState = (typeof REALTIME_STATES)[number]

/** 断线重连退避起点（技术方案 5.2：指数退避 1s 起）。 */
export const BACKOFF_INITIAL_MS = 1_000

/** 断线重连退避上限（技术方案 5.2：上限 30s）。 */
export const BACKOFF_MAX_MS = 30_000

export interface StateTransition {
  readonly from: RealtimeState
  readonly to: RealtimeState
  /** 迁移时刻（注入时钟的 now）。 */
  readonly atMs: number
}

/** 接入房间所需的业务键（W-01 join 载荷）。 */
export interface RoomSelector {
  readonly base_id: string
  readonly table_id: string
}

export type Unsubscribe = () => void

export interface RealtimeClientOptions {
  transportFactory: TransportFactory
  /** 定时器/时钟注入面；缺省 realScheduler。 */
  scheduler?: Scheduler
  /** 心跳周期；缺省 15s。 */
  heartbeatIntervalMs?: number
  backoffInitialMs?: number
  backoffMaxMs?: number
}

export type ServerMessageListener = (message: ServerMessage) => void
export type StateListener = (transition: StateTransition) => void

export class RealtimeClient {
  private readonly transportFactory: TransportFactory
  private readonly scheduler: Scheduler
  private readonly backoffInitialMs: number
  private readonly backoffMaxMs: number
  private readonly heartbeat: HeartbeatMonitor

  private state: RealtimeState = 'idle'
  private room: RoomSelector | null = null
  private lastSeq: number | null = null
  private transport: Transport | null = null
  private reconnectTimer: number | null = null
  private backoffAttempt = 0
  private readonly stateListeners = new Set<StateListener>()
  private readonly messageListeners = new Set<ServerMessageListener>()
  private readonly typedListeners = new Map<ServerMessageName, Set<ServerMessageListener>>()

  constructor(options: RealtimeClientOptions) {
    this.transportFactory = options.transportFactory
    this.scheduler = options.scheduler ?? realScheduler
    this.backoffInitialMs = options.backoffInitialMs ?? BACKOFF_INITIAL_MS
    this.backoffMaxMs = options.backoffMaxMs ?? BACKOFF_MAX_MS
    this.heartbeat = new HeartbeatMonitor({
      scheduler: this.scheduler,
      intervalMs: options.heartbeatIntervalMs,
      sendHeartbeat: () => {
        this.sendRaw(encodeClientMessage({ t: 'heartbeat' }))
      },
      onTimeout: () => {
        this.handleConnectionLost()
      },
    })
  }

  // ---------- 查询 ----------

  getState(): RealtimeState {
    return this.state
  }

  /** 本地 seq 高水位（对账字段；断线重连 join/replay_request 携带）。 */
  getLastSeq(): number | null {
    return this.lastSeq
  }

  // ---------- 状态订阅 ----------

  onStateChange(listener: StateListener): Unsubscribe {
    this.stateListeners.add(listener)
    return () => this.offStateChange(listener)
  }

  offStateChange(listener: StateListener): void {
    this.stateListeners.delete(listener)
  }

  /** 泛型订阅：全部 S→C 类型化消息。 */
  onMessage(listener: ServerMessageListener): Unsubscribe
  /** 按消息名订阅：只收指定类型的类型化消息。 */
  onMessage<K extends ServerMessageName>(type: K, listener: (message: ServerMessageMap[K]) => void): Unsubscribe
  onMessage(typeOrListener: ServerMessageName | ServerMessageListener, maybeListener?: ServerMessageListener): Unsubscribe {
    if (typeof typeOrListener === 'function') {
      this.messageListeners.add(typeOrListener)
      return () => this.offMessage(typeOrListener)
    }
    const type = typeOrListener
    const listener = maybeListener as ServerMessageListener
    let set = this.typedListeners.get(type)
    if (set === undefined) {
      set = new Set()
      this.typedListeners.set(type, set)
    }
    set.add(listener)
    return () => this.offMessage(type, listener)
  }

  offMessage(listener: ServerMessageListener): void
  offMessage<K extends ServerMessageName>(type: K, listener: (message: ServerMessageMap[K]) => void): void
  offMessage(typeOrListener: ServerMessageName | ServerMessageListener, maybeListener?: ServerMessageListener): void {
    if (typeof typeOrListener === 'function') {
      this.messageListeners.delete(typeOrListener)
      return
    }
    this.typedListeners.get(typeOrListener)?.delete(maybeListener as ServerMessageListener)
  }

  // ---------- 生命周期 ----------

  /**
   * 接入 Base 房间：idle → connecting，并经注入的工厂建立传输。
   * 仅 idle 态可调用；revoked 为终态（调用被忽略）；其余态视为程序错误。
   */
  connect(room: RoomSelector): void {
    if (this.state === 'revoked') {
      return
    }
    if (this.state !== 'idle') {
      throw new Error(`RealtimeClient.connect：仅 idle 态可调用（当前 ${this.state}）`)
    }
    this.room = room
    this.backoffAttempt = 0
    this.transition('connecting')
    this.openTransport()
  }

  /** 主动断开：joined 先发 leave，随后回 idle 并关闭传输；revoked/idle 幂等。 */
  disconnect(): void {
    if (this.state === 'revoked' || this.state === 'idle') {
      return
    }
    if (this.state === 'joined') {
      this.sendRaw(encodeClientMessage({ t: 'leave' }))
    }
    this.heartbeat.stop()
    this.clearReconnectTimer()
    this.transition('idle')
    this.closeTransport()
  }

  /**
   * 业务消息发送门控：仅 joined 态放行（技术方案 5.2 对账——异常期间不盲发业务消息）。
   * 被拒时返回 false（内容不排队，离线补发属票 06 离线队列职责）。
   */
  send(message: ClientMessage): boolean {
    if (this.state !== 'joined') {
      return false
    }
    return this.sendRaw(encodeClientMessage(message))
  }

  // ---------- 传输回调（注入工厂的 handlers） ----------

  private readonly transportHandlers: TransportHandlers = {
    onOpen: () => this.handleTransportOpen(),
    onMessage: (raw) => this.handleTransportMessage(raw),
    onClose: (info) => this.handleTransportClose(info),
  }

  private openTransport(): void {
    this.transport = this.transportFactory(this.transportHandlers)
    this.transport.connect()
  }

  private handleTransportOpen(): void {
    if (this.state === 'revoked' || this.room === null) {
      return
    }
    // 连接建立即发 join；last_seq 携本地高水位（首次接入缺省）。
    const join: ClientMessage = {
      t: 'join',
      base_id: this.room.base_id,
      table_id: this.room.table_id,
      ...(this.lastSeq === null ? {} : { last_seq: this.lastSeq }),
    }
    this.sendRaw(encodeClientMessage(join))
  }

  private handleTransportMessage(raw: string): void {
    if (this.state === 'revoked') {
      return
    }
    const message = parseServerMessage(raw)
    if (message === null) {
      return
    }
    this.applyMessage(message)
    this.dispatchMessage(message)
  }

  private handleTransportClose(_info: TransportCloseInfo): void {
    this.handleConnectionLost()
  }

  // ---------- 控制面：消息的控制效果 ----------

  private applyMessage(message: ServerMessage): void {
    switch (message.t) {
      case 'snapshot': {
        const wasReconnecting = this.state === 'reconnecting'
        const priorSeq = this.lastSeq
        this.bumpLastSeq(message.seq)
        if (this.state === 'connecting' || this.state === 'reconnecting') {
          this.backoffAttempt = 0
          this.transition('joined')
          this.heartbeat.start()
          if (wasReconnecting && priorSeq !== null) {
            // 重连成功：请求补发本地缺失的增量（技术方案 5.2 对账）。
            this.sendRaw(encodeClientMessage({ t: 'replay_request', last_seq: priorSeq }))
          }
        }
        return
      }
      case 'cell_changed':
      case 'record_deleted':
      case 'structure_changed':
        this.bumpLastSeq(message.seq)
        return
      case 'ack':
        this.heartbeat.onAck()
        return
      case 'session_revoked':
        this.revoke()
        return
      default:
        return
    }
  }

  private bumpLastSeq(seq: number): void {
    this.lastSeq = this.lastSeq === null ? seq : Math.max(this.lastSeq, seq)
  }

  private dispatchMessage(message: ServerMessage): void {
    for (const listener of this.messageListeners) {
      listener(message)
    }
    const typed = this.typedListeners.get(message.t)
    if (typed !== undefined) {
      for (const listener of typed) {
        listener(message)
      }
    }
  }

  // ---------- 控制面：断线与重连 ----------

  private handleConnectionLost(): void {
    if (this.state === 'revoked' || this.state === 'idle') {
      return
    }
    this.heartbeat.stop()
    this.transition('reconnecting')
    this.scheduleReconnect()
  }

  private scheduleReconnect(): void {
    this.clearReconnectTimer()
    const delay = Math.min(this.backoffInitialMs * 2 ** this.backoffAttempt, this.backoffMaxMs)
    this.backoffAttempt += 1
    this.reconnectTimer = this.scheduler.setTimeout(() => {
      this.reconnectTimer = null
      if (this.state === 'revoked' || this.state === 'idle') {
        return
      }
      this.openTransport()
    }, delay)
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer !== null) {
      this.scheduler.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }

  // ---------- 控制面：吊销终态 ----------

  private revoke(): void {
    if (this.state === 'revoked') {
      return
    }
    this.heartbeat.stop()
    this.clearReconnectTimer()
    this.transition('revoked')
    this.closeTransport()
  }

  // ---------- 内部 ----------

  private transition(to: RealtimeState): void {
    if (this.state === to || this.state === 'revoked') {
      return
    }
    const transition: StateTransition = { from: this.state, to, atMs: this.scheduler.now() }
    this.state = to
    for (const listener of this.stateListeners) {
      listener(transition)
    }
  }

  /** 底层帧发送：传输异常视同断线（转 reconnecting），返回是否成功。 */
  private sendRaw(raw: string): boolean {
    try {
      this.transport?.send(raw)
      return true
    } catch {
      this.handleConnectionLost()
      return false
    }
  }

  private closeTransport(): void {
    const transport = this.transport
    this.transport = null
    try {
      transport?.close()
    } catch {
      // 关闭失败不改变状态机走向（终态/idle 已定型）。
    }
  }
}
