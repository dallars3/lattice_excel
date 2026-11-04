// 离线队列状态机（票 06，ADR 0002）：断线只读/在途保留/重放/冲突转 4001 裁决/滞留提示。
// 纯逻辑引擎（DOM 无关）；zustand 接线见 src/stores/offlineQueue.ts。
export {
  CONFLICT_ACTIONS,
  ITEM_STATUSES,
  QUEUE_PHASES,
  STALL_THRESHOLD_MS,
  OfflineQueueEngine,
  type ConflictAction,
  type EnqueueInput,
  type EnqueueOutcome,
  type ItemStatus,
  type OfflineQueueEngineOptions,
  type OfflineQueueItem,
  type OfflineQueueItemContent,
  type OfflineQueueSnapshot,
  type OfflineQueueSender,
  type OfflineQueueTarget,
  type QueuePhase,
  type ReplayProgress,
  type SendResult,
  type StallNotification,
} from './queue'
