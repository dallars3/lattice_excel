import { create } from 'zustand'

/** 离线队列项（client_request_id 由票 04 生成器统一发放）。 */
export interface OfflineQueueItem {
  readonly clientRequestId: string
}

/** 重放进度（重放期间非空）。 */
export interface ReplayProgress {
  readonly done: number
  readonly total: number
}

/** offlineQueueStore（技术方案第 8 章）：待传队列、重放进度、滞留计时。 */
export interface OfflineQueueState {
  /** 待重放的离线队列（顺序即重放顺序） */
  readonly pendingItems: readonly OfflineQueueItem[]
  /** 当前重放进度 */
  readonly replayProgress: ReplayProgress | null
}

/** 空骨架（票 02）：仅初始状态供任意组件消费；入队/断线只读/重放/冲突裁决状态机随票 06 落地。 */
export const offlineQueueStore = create<OfflineQueueState>()(() => ({
  pendingItems: [],
  replayProgress: null,
}))
