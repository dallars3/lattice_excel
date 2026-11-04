// offlineQueueStore（票 06，技术方案 5.3 / 第 8 章）：待传队列、重放进度、滞留计时 + 状态机动作。
// 消费方：banner（离线横幅/未同步清单入口）、grid。
// 多标签隔离取舍（票面）：跨标签页天然是独立 JS 运行时（每标签各一份模块实例），页内 singleton
// 供 banner/grid 直接 import 消费；实例隔离语义由工厂 createOfflineQueueStore 承载——
// 两个实例互不可见对方队列（用例锁死），测试经工厂注入 Fake sender/clock，不触真网络（spec）。
import { StoreApi, create, UseBoundStore } from 'zustand'

import {
  OfflineQueueEngine,
  type ConflictAction,
  type EnqueueInput,
  type EnqueueOutcome,
  type OfflineQueueEngineOptions,
  type OfflineQueueItemContent,
  type OfflineQueueSnapshot,
  type OfflineQueueSender,
} from '@/core/offline'

// 领域类型归口 core/offline（队列项/相位/进度等），store 层透传导出给消费方。
export type {
  ConflictAction,
  EnqueueInput,
  EnqueueOutcome,
  ItemStatus,
  OfflineQueueItem,
  OfflineQueueItemContent,
  OfflineQueueSender,
  OfflineQueueTarget,
  QueuePhase,
  ReplayProgress,
  SendResult,
  StallNotification,
} from '@/core/offline'

/** offlineQueueStore 状态 + 动作（技术方案第 8 章口径）。 */
export interface OfflineQueueState extends OfflineQueueSnapshot {
  /** 入队：在线即发；断线只读时明确拒绝（不静默吞）。 */
  enqueue(input: EnqueueInput): EnqueueOutcome
  /** 进入离线只读（断线事件由 M3 装配层驱动）。 */
  goOffline(): void
  /** 重连重放：先补增量（onReconnectSync 钩子）再顺序 flush；冲突暂停待裁决。 */
  reconnect(): Promise<void>
  /** 4001 三动作裁决（FR-06-3：use_other / overwrite / copy）。 */
  resolveConflict(clientRequestId: string, action: ConflictAction): void
  /** 失败/冲突项内容取回（「未同步清单复制入口」的事实源）。 */
  getItemContent(clientRequestId: string): OfflineQueueItemContent | null
  /** 派发收口：等待当前 flush 循环退出（装配层/测试用）。 */
  flushPending(): Promise<void>
}

export type OfflineQueueStore = UseBoundStore<StoreApi<OfflineQueueState>>

export interface CreateOfflineQueueStoreOptions extends Partial<Omit<OfflineQueueEngineOptions, 'sender'>> {
  /** 发送接缝；M1 测试注入 Fake sender，M3 装配真实现。 */
  sender?: OfflineQueueSender
}

/** M1 不与真网络联动（spec）：未装配 sender 时的占位——显式 error 结果，绝不静默成功。 */
const notConfiguredSender: OfflineQueueSender = async () => ({
  kind: 'error',
  message: 'offlineQueue sender 未配置（M1 不接真网络；M3 装配时注入真实现）',
})

/** 工厂：创建相互隔离的离线队列实例（多标签语义可测；多标签页=独立模块实例+独立会话）。 */
export function createOfflineQueueStore(options: CreateOfflineQueueStoreOptions = {}): OfflineQueueStore {
  const engine = new OfflineQueueEngine({
    sender: options.sender ?? notConfiguredSender,
    onReconnectSync: options.onReconnectSync,
    scheduler: options.scheduler,
    generateRequestId: options.generateRequestId,
    stallThresholdMs: options.stallThresholdMs,
    onStall: options.onStall,
  })
  const store = create<OfflineQueueState>()(() => ({
    ...engine.getSnapshot(),
    enqueue: (input) => engine.enqueue(input),
    goOffline: () => engine.goOffline(),
    reconnect: () => engine.reconnect(),
    resolveConflict: (clientRequestId, action) => engine.resolveConflict(clientRequestId, action),
    getItemContent: (clientRequestId) => engine.getItemContent(clientRequestId),
    flushPending: () => engine.flushPending(),
  }))
  // 引擎状态镜像进 zustand（动作闭包持有引擎，状态数据每次变更整体刷新；动作引用保持稳定）。
  engine.subscribe(() => {
    store.setState({ ...engine.getSnapshot() })
  })
  return store
}

/** 页内单例（技术方案第 8 章挂载点，票 02 骨架转正）：banner/grid 直接消费。 */
export const offlineQueueStore = createOfflineQueueStore()
