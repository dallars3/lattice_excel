// 心跳（票 05，技术方案 5.2）：joined 态每 15s 发一次 heartbeat（W-03）；
// 收到 ack（W-16）仅解除「未应答」标记（超时判定重置），周期本身固定；
// 若一个完整周期内无应答，下一 tick 判死并回调 onTimeout（由状态机转 reconnecting）。
// 时间行为全部经注入的 Scheduler，测试无真实等待。
import type { Scheduler } from './scheduler'

export const HEARTBEAT_INTERVAL_MS = 15_000

export interface HeartbeatMonitorOptions {
  scheduler: Scheduler
  intervalMs?: number
  sendHeartbeat: () => void
  onTimeout: () => void
}

export class HeartbeatMonitor {
  private readonly scheduler: Scheduler
  private readonly intervalMs: number
  private readonly sendHeartbeat: () => void
  private readonly onTimeout: () => void
  private timer: ReturnType<Scheduler['setTimeout']> | null = null
  private awaitingAck = false
  private running = false

  constructor(options: HeartbeatMonitorOptions) {
    this.scheduler = options.scheduler
    this.intervalMs = options.intervalMs ?? HEARTBEAT_INTERVAL_MS
    this.sendHeartbeat = options.sendHeartbeat
    this.onTimeout = options.onTimeout
  }

  /** 进入 joined 态启动周期；重复 start 幂等。 */
  start(): void {
    if (this.running) {
      return
    }
    this.running = true
    this.awaitingAck = false
    this.scheduleTick()
  }

  /** 离开 joined / 判死 / 吊销时停止；未启动时幂等。 */
  stop(): void {
    if (this.timer !== null) {
      this.scheduler.clearTimeout(this.timer)
      this.timer = null
    }
    this.running = false
    this.awaitingAck = false
  }

  /** 收到 W-16 ack：解除未应答标记（下一 tick 照常续发）。 */
  onAck(): void {
    this.awaitingAck = false
  }

  private scheduleTick(): void {
    this.timer = this.scheduler.setTimeout(() => this.tick(), this.intervalMs)
  }

  private tick(): void {
    this.timer = null
    if (!this.running) {
      return
    }
    if (this.awaitingAck) {
      // 上个周期无应答：判死，不再排程；后续由状态机决定（转 reconnecting）。
      this.onTimeout()
      return
    }
    this.sendHeartbeat()
    this.awaitingAck = true
    this.scheduleTick()
  }
}
