// 离线队列状态机（票 06，ADR 0002 落地 / 技术方案 5.3 / 需求文档 4.6 离线模型）。
// 语义唯一事实源：
// - ADR 0002：断线只读、在途项保留、重连后先补增量（replay_request, W-07）再顺序重传本地队列、
//   逐条入口可感知成功/冲突（冲突走 FR-06-3 三动作）、任何失败项不丢弃可复制。
// - 技术方案 5.3：写提交先落 offlineQueue（携带 client_request_id、目标资源、编辑前快照），
//   网络正常即发；队首滞留 >5 分钟触发「未同步清单 + 复制入口」提示。
// - 需求文档 4.6：断网离线只读（不产生新编辑）；恢复自动重连先补增量再顺序重传；>5 分钟提示刷新。
// M1 不与真网络联动：发送经注入的 sender 接口（测试用 Fake sender），时钟经 Scheduler 注入
// （复用票 05 接缝），零真实等待；真网络联动在 M3 换入实现，状态机零改动。
//
// 派发语义（顺序保证）：严格 FIFO——队首未结算项才有发送资格；失败项按原位停止本段派发
// （后续项不越过），下次 flush 触发（重连/在线新入队）先重试失败项；冲突项暂停派发等三动作裁决。
import { createRequestId } from '@/core/http/idempotency'
import type { Scheduler } from '@/core/realtime/scheduler'
import { realScheduler } from '@/core/realtime/scheduler'

// ---------- 队列级状态 ----------

/** 队列相位：idle（在线待命，网络正常即发）→ offline（断线只读）→ replaying（重连后重放中）。 */
export const QUEUE_PHASES = ['idle', 'offline', 'replaying'] as const
export type QueuePhase = (typeof QUEUE_PHASES)[number]

/** 队列项状态（逐条生命周期）。 */
export const ITEM_STATUSES = ['queued', 'sending', 'synced', 'failed', 'conflict', 'resolved'] as const
export type ItemStatus = (typeof ITEM_STATUSES)[number]

/** 4001 冲突三动作（FR-06-3，禁止静默覆盖）。 */
export const CONFLICT_ACTIONS = ['use_other', 'overwrite', 'copy'] as const
export type ConflictAction = (typeof CONFLICT_ACTIONS)[number]

/** 队首滞留阈值（技术方案 5.3：>5 分钟触发未同步清单提示）。 */
export const STALL_THRESHOLD_MS = 5 * 60 * 1000

// ---------- 队列项 ----------

/** 目标资源：逻辑写操作 + 资源路径（技术方案 5.3：UpdateCell/BatchCreate/DeleteRecords 等写提交）。 */
export interface OfflineQueueTarget {
  /** 逻辑写操作名（如 update_cell / create_records / delete_records）。 */
  readonly kind: string
  /** 目标资源路径（M3 真 sender 消费；M1 仅作为载荷透传）。 */
  readonly url: string
  readonly body?: unknown
}

/**
 * 离线队列项。失败/冲突项不丢弃：snapshot（编辑前快照）+ localValue（本地值）恒保留，
 * 内容经 getItemContent 取回（「复制入口」的事实源）。
 */
export interface OfflineQueueItem {
  /** 幂等键：复用票 04 生成器口径；失败重试/覆盖重发沿用同一 id（接口文档 8.0.2）。 */
  readonly clientRequestId: string
  readonly target: OfflineQueueTarget
  /** 编辑前快照（内容可复制不丢失的一半）。 */
  readonly snapshot: unknown
  /** 本地值（内容可复制不丢失的另一半）。 */
  readonly localValue: unknown
  readonly status: ItemStatus
  /** 冲突裁决动作（仅 resolved 态非空）。 */
  readonly resolution: ConflictAction | null
  /** 4001 冲突时服务端值（use_other 采用；M3 覆盖重发时携其 version）。 */
  readonly serverValue: unknown
  /** 已发送次数：即发 1 次；重放/覆盖重发递增。 */
  readonly attempts: number
  /** 入队时刻（注入时钟毫秒，统一毫秒时间戳口径）。 */
  readonly enqueuedAtMs: number
  /** 最近一次状态变更时刻。 */
  readonly updatedAtMs: number
}

/** 重放进度（重放期间非空；done 按本段重放会话内已结算计数）。 */
export interface ReplayProgress {
  readonly done: number
  readonly total: number
}

/** 内容取回形态（「未同步清单复制入口」/失败项取回的事实源）。 */
export type OfflineQueueItemContent = Pick<
  OfflineQueueItem,
  'clientRequestId' | 'target' | 'snapshot' | 'localValue' | 'status'
>

// ---------- 注入面 ----------

export type SendResult =
  | { kind: 'success' }
  /** 4001 版本冲突：serverValue 为服务端当前值（三动作裁决依据）。 */
  | { kind: 'conflict'; serverValue?: unknown }
  | { kind: 'error'; message?: string }

/** 发送接缝：M1 测试用 Fake sender；M3 换真实现（经 httpClient 携幂等 id 发送）。 */
export type OfflineQueueSender = (
  item: OfflineQueueItem,
  options: { overwrite: boolean },
) => Promise<SendResult> | SendResult

export interface EnqueueInput {
  readonly target: OfflineQueueTarget
  /** 编辑前快照；无（新建类写）可空。 */
  readonly snapshot?: unknown
  readonly localValue: unknown
  /** 缺省由票 04 生成器（createRequestId）发放。 */
  readonly clientRequestId?: string
}

export type EnqueueOutcome =
  | { accepted: true; item: OfflineQueueItem }
  /** 断线只读：新编辑被明确拒绝（需求 4.6），不静默吞。 */
  | { accepted: false; reason: 'offline_readonly' }

/** 队首滞留提示流（事件/回调表达；UI 消费是后续票/M3 的事）。 */
export interface StallNotification {
  /** 滞留的队首项。 */
  readonly head: OfflineQueueItem
  /** 未同步清单（全部未结算项；内容经 getItemContent 复制取回）。 */
  readonly unsyncedItems: readonly OfflineQueueItem[]
  readonly stalledMs: number
}

export interface OfflineQueueEngineOptions {
  /** 发送接缝（必注入）；M1 全部经 Fake sender 驱动。 */
  sender: OfflineQueueSender
  /** 重连成功后的补增量钩子：先于任何队列重放被 await（ADR 0002「先补增量再顺序重放」的顺序约定）。 */
  onReconnectSync?: () => Promise<void> | void
  /** 时钟/定时器接缝（复用票 05 Scheduler）；缺省 realScheduler。 */
  scheduler?: Scheduler
  /** 幂等 id 生成器；缺省票 04 的 createRequestId。 */
  generateRequestId?: () => string
  /** 队首滞留阈值；缺省 5 分钟。 */
  stallThresholdMs?: number
  /** 滞留提示流回调；缺省无（订阅式测试直接 addStallListener）。 */
  onStall?: (notification: StallNotification) => void
}

// ---------- 快照 ----------

export interface OfflineQueueSnapshot {
  readonly phase: QueuePhase
  /** 全部队列项（入队顺序；含已结算项——逐条入口可感知成功/冲突）。 */
  readonly items: readonly OfflineQueueItem[]
  /** 未结算项（顺序即重放顺序）——banner「离线未同步」计数的事实源。 */
  readonly pendingItems: readonly OfflineQueueItem[]
  readonly replayProgress: ReplayProgress | null
}

const UNSETTLED: readonly ItemStatus[] = ['queued', 'sending', 'failed', 'conflict']

// ---------- 状态机 ----------

/**
 * 离线队列状态机（纯逻辑，DOM 无关）：
 * - 入队：在线（idle/replaying）即发；断线（offline）拒绝新编辑并保留既有项。
 * - 重放：reconnect 先 await 补增量钩子，再严格 FIFO 逐条派发，逐条回填成功/冲突/失败。
 * - 冲突：项转 conflict 并暂停派发，等待三动作裁决（resolveConflict）。
 * - 滞留：队首项滞留 >5min 触发一次「未同步清单」提示流（注入计时器）。
 */
export class OfflineQueueEngine {
  private readonly sender: OfflineQueueSender
  private readonly onReconnectSync: (() => Promise<void> | void) | null
  private readonly scheduler: Scheduler
  private readonly generateRequestId: () => string
  private readonly stallThresholdMs: number
  private readonly onStallOption: ((notification: StallNotification) => void) | null

  private phase: QueuePhase = 'idle'
  private items: OfflineQueueItem[] = []
  private replayProgress: ReplayProgress | null = null
  /** 当前重放会话口径（reconnect 时刻冻结）。 */
  private replayTotal = 0
  private replayDone = 0

  private flushPromise: Promise<void> | null = null
  /** 在途发送（clientRequestId → 未决 sendItem promise）：断线时保留，重放等待其回填。 */
  private readonly inFlight = new Map<string, Promise<SendResult>>()
  /** 覆盖重发（resolveConflict 'overwrite'）：重发时以 overwrite=true 交 sender（M3 携远端 version）。 */
  private readonly overwritePending = new Set<string>()

  private readonly changeListeners = new Set<() => void>()
  private readonly stallListeners = new Set<(notification: StallNotification) => void>()
  /** 队首滞留计时器（注入 Scheduler；队首 episode 内单次）。 */
  private stallTimer: number | null = null
  /** 已提示过的队首（同一队首滞留只提示一次，队首更换后重新武装）。 */
  private stallNotifiedFor: string | null = null

  constructor(options: OfflineQueueEngineOptions) {
    this.sender = options.sender
    this.onReconnectSync = options.onReconnectSync ?? null
    this.scheduler = options.scheduler ?? realScheduler
    this.generateRequestId = options.generateRequestId ?? createRequestId
    this.stallThresholdMs = options.stallThresholdMs ?? STALL_THRESHOLD_MS
    this.onStallOption = options.onStall ?? null
  }

  // ---------- 查询与订阅 ----------

  getSnapshot(): OfflineQueueSnapshot {
    const pendingItems = this.items.filter((item) => UNSETTLED.includes(item.status))
    return { phase: this.phase, items: [...this.items], pendingItems, replayProgress: this.replayProgress }
  }

  /**
   * 失败/冲突项内容取回接口：编辑前快照 + 本地值恒保留（ADR 0002「失败项内容不丢失」），
   * 是「未同步清单复制入口」与「4002 记录被删保留内容」的事实源。
   */
  getItemContent(clientRequestId: string): OfflineQueueItemContent | null {
    const item = this.items.find((candidate) => candidate.clientRequestId === clientRequestId)
    if (item === undefined) {
      return null
    }
    return {
      clientRequestId: item.clientRequestId,
      target: item.target,
      snapshot: item.snapshot,
      localValue: item.localValue,
      status: item.status,
    }
  }

  subscribe(listener: () => void): () => void {
    this.changeListeners.add(listener)
    return () => {
      this.changeListeners.delete(listener)
    }
  }

  addStallListener(listener: (notification: StallNotification) => void): () => void {
    this.stallListeners.add(listener)
    return () => {
      this.stallListeners.delete(listener)
    }
  }

  // ---------- 入队 ----------

  enqueue(input: EnqueueInput): EnqueueOutcome {
    if (this.phase === 'offline') {
      // 断线只读（需求 4.6）：明确拒绝，不静默吞；既有在途项与已入队项不受影响。
      return { accepted: false, reason: 'offline_readonly' }
    }
    const nowMs = this.scheduler.now()
    const item: OfflineQueueItem = {
      clientRequestId: input.clientRequestId ?? this.generateRequestId(),
      target: input.target,
      snapshot: input.snapshot,
      localValue: input.localValue,
      status: 'queued',
      resolution: null,
      serverValue: undefined,
      attempts: 0,
      enqueuedAtMs: nowMs,
      updatedAtMs: nowMs,
    }
    this.items.push(item)
    this.emit()
    // 网络正常即发（技术方案 5.3）：派发为 fire-and-forget，结果逐条回填。
    void this.dispatch()
    return { accepted: true, item }
  }

  // ---------- 相位控制：断线 / 重连重放 ----------

  /** 进入断线（离线只读）：新编辑此后被拒；既有在途项与已入队项保留。幂等。 */
  goOffline(): void {
    if (this.phase === 'offline') {
      return
    }
    this.transition('offline')
    // 重放会话挂起：进度清空，横幅口径切「离线未同步」计数（pendingItems）。
    this.replayProgress = null
    this.emit()
  }

  /**
   * 重连恢复：offline → replaying，先 await 补增量钩子（ADR 0002「先补增量再顺序重放」），
   * 再严格 FIFO 逐条重放；全部完成回 idle。补增量失败则会话中止回 offline 并向上抛错。
   */
  async reconnect(): Promise<void> {
    if (this.phase !== 'offline') {
      return // idle 无需重放；replaying 中（冲突暂停/在途回填）不重复触发。
    }
    this.transition('replaying')
    this.replayTotal = this.items.filter((item) => UNSETTLED.includes(item.status)).length
    this.replayDone = 0
    this.replayProgress = { done: 0, total: this.replayTotal }
    this.emit()
    if (this.onReconnectSync !== null) {
      try {
        await this.onReconnectSync()
      } catch (error) {
        // 补增量失败：会话中止回离线（队列保留），错误上抛不静默吞；下次重连再试。
        this.transition('offline')
        this.replayProgress = null
        this.emit()
        throw error
      }
    }
    await this.dispatch()
    this.closeReplaySessionIfDone()
  }

  // ---------- 冲突裁决（FR-06-3：① 用对方的值 ② 仍用我的覆盖 ③ 复制我的内容） ----------

  /**
   * 4001 三动作裁决：仅冲突态可裁决（否则明确抛错，不静默吞）。
   * - use_other：采用服务端值，丢弃本地编辑（serverValue 已记录在项上）；
   * - overwrite：强制覆盖重发（同幂等 id，sender 收 overwrite=true，M3 携远端 version）；
   * - copy：另存为副本——原项标记冲突已处理，原始内容经 getItemContent 保留可取回。
   * 裁决后在线续跑派发；重放会话若就此全部完成则收口回 idle。
   */
  resolveConflict(clientRequestId: string, action: ConflictAction): void {
    const item = this.items.find((candidate) => candidate.clientRequestId === clientRequestId)
    if (item === undefined || item.status !== 'conflict') {
      throw new Error(`resolveConflict：项 ${clientRequestId} 不存在或不在冲突态，不可裁决`)
    }
    const nowMs = this.scheduler.now()
    if (action === 'overwrite') {
      this.overwritePending.add(clientRequestId)
      this.updateItem(clientRequestId, { status: 'queued', updatedAtMs: nowMs })
    } else {
      this.updateItem(clientRequestId, { status: 'resolved', resolution: action, updatedAtMs: nowMs })
      this.noteSettle()
    }
    this.emit()
    void this.dispatch().then(() => this.closeReplaySessionIfDone())
  }

  // ---------- 派发（在线即发与重放共用；单飞） ----------

  /** 派发收口：加入当前 flush 循环（在跑则等它），测试据此确定性收口——零真实等待。 */
  flushPending(): Promise<void> {
    return this.dispatch()
  }

  private dispatch(): Promise<void> {
    if (this.flushPromise !== null) {
      return this.flushPromise
    }
    const promise: Promise<void> = this.flushLoop().finally(() => {
      if (this.flushPromise === promise) {
        this.flushPromise = null
      }
    })
    this.flushPromise = promise
    return promise
  }

  private async flushLoop(): Promise<void> {
    for (;;) {
      if (this.phase === 'offline') {
        return // 断线暂停派发；在途项保留，结果照常回填。
      }
      const head = this.firstUnsettledItem()
      if (head === undefined) {
        return
      }
      if (head.status === 'conflict') {
        return // 冲突转裁决（FR-06-3）：暂停派发，等 resolveConflict。
      }
      if (head.status === 'sending') {
        // 在途项（断线前已发出、尚未回填）：等它回填后再继续顺序派发，不重复发送。
        const pending = this.inFlight.get(head.clientRequestId)
        if (pending === undefined) {
          return
        }
        await pending
        continue
      }
      // queued | failed → 严格 FIFO 顺序发送。
      const result = await this.sendItem(head, this.overwritePending.has(head.clientRequestId))
      if (result.kind !== 'success') {
        return // 失败保序停止（failed 留待下次触发重试）/ 冲突暂停（待裁决）。
      }
    }
  }

  private firstUnsettledItem(): OfflineQueueItem | undefined {
    return this.items.find((item) => UNSETTLED.includes(item.status))
  }

  private async sendItem(item: OfflineQueueItem, overwrite: boolean): Promise<SendResult> {
    this.updateItem(item.clientRequestId, { status: 'sending', attempts: item.attempts + 1 })
    this.emit()
    const pending = (async (): Promise<SendResult> => {
      try {
        return await this.sender(item, { overwrite })
      } catch (error) {
        // sender 抛异常视同发送失败（错误不静默吞，转为显式 failed 态）。
        return { kind: 'error', message: error instanceof Error ? error.message : String(error) }
      }
    })()
    this.inFlight.set(item.clientRequestId, pending)
    const result = await pending
    this.inFlight.delete(item.clientRequestId)
    this.applySendResult(item, result)
    return result
  }

  private applySendResult(item: OfflineQueueItem, result: SendResult): void {
    const nowMs = this.scheduler.now()
    if (result.kind === 'success') {
      this.overwritePending.delete(item.clientRequestId)
      this.updateItem(item.clientRequestId, { status: 'synced', updatedAtMs: nowMs })
      this.noteSettle()
    } else if (result.kind === 'conflict') {
      this.updateItem(item.clientRequestId, { status: 'conflict', serverValue: result.serverValue, updatedAtMs: nowMs })
    } else {
      this.updateItem(item.clientRequestId, { status: 'failed', updatedAtMs: nowMs })
    }
    this.emit()
  }

  // ---------- 内部 ----------

  private transition(to: QueuePhase): void {
    if (this.phase === to) {
      return
    }
    this.phase = to
  }

  /** 重放会话内结算计数（done 推进；新入队项不在会话 total 口径内，钳位防越界显示）。 */
  private noteSettle(): void {
    if (this.phase !== 'replaying') {
      return
    }
    this.replayDone += 1
    this.replayProgress = { done: Math.min(this.replayDone, this.replayTotal), total: this.replayTotal }
  }

  /** 重放会话收口：全部完成（或以失败收口）→ idle、进度清空。 */
  private endReplaySession(): void {
    this.transition('idle')
    this.replayProgress = null
    this.emit()
  }

  /** 派发退出后的会话收口判定：冲突暂停（队首待裁决）保持 replaying，其余收口。 */
  private closeReplaySessionIfDone(): void {
    if (this.phase !== 'replaying') {
      return
    }
    const head = this.firstUnsettledItem()
    if (head === undefined || head.status !== 'conflict') {
      this.endReplaySession()
    }
  }

  private updateItem(clientRequestId: string, patch: Partial<OfflineQueueItem>): void {
    const index = this.items.findIndex((candidate) => candidate.clientRequestId === clientRequestId)
    if (index >= 0) {
      this.items[index] = { ...this.items[index], ...patch }
    }
  }

  private emit(): void {
    this.rearmStallTimer()
    for (const listener of this.changeListeners) {
      listener()
    }
  }

  // ---------- 滞留提示（技术方案 5.3：队首滞留 >5 分钟 → 未同步清单 + 复制入口提示） ----------

  /** 每次状态变更后重算滞留计时：队首未结算项按其入队时刻 + 阈值（绝对时刻，重算不漂移）。 */
  private rearmStallTimer(): void {
    const head = this.firstUnsettledItem()
    if (head === undefined) {
      this.disarmStallTimer()
      this.stallNotifiedFor = null
      return
    }
    if (head.clientRequestId === this.stallNotifiedFor) {
      return // 同一队首 episode 已提示过，不重复武装。
    }
    this.disarmStallTimer()
    const delayMs = Math.max(0, head.enqueuedAtMs + this.stallThresholdMs - this.scheduler.now())
    this.stallTimer = this.scheduler.setTimeout(() => this.fireStall(), delayMs)
  }

  private disarmStallTimer(): void {
    if (this.stallTimer !== null) {
      this.scheduler.clearTimeout(this.stallTimer)
      this.stallTimer = null
    }
  }

  private fireStall(): void {
    this.stallTimer = null
    const head = this.firstUnsettledItem()
    if (head === undefined || head.clientRequestId === this.stallNotifiedFor) {
      return
    }
    this.stallNotifiedFor = head.clientRequestId
    const notification: StallNotification = {
      head,
      unsyncedItems: this.items.filter((item) => UNSETTLED.includes(item.status)),
      stalledMs: Math.max(0, this.scheduler.now() - head.enqueuedAtMs),
    }
    // 提示流以事件/回调表达：onStall 选项 + addStallListener 双通道；「复制入口」由
    // getItemContent / 项内 snapshot+localValue 供 UI 取用（M1 无 UI）。
    if (this.onStallOption !== null) {
      this.onStallOption(notification)
    }
    for (const listener of this.stallListeners) {
      listener(notification)
    }
  }
}
