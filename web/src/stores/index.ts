// 6 个 Zustand store 空骨架（票 02）：命名唯一事实源为《前端-技术方案-v0.1.md》第 8 章清单。
// 模块级单例，无需 Provider——任意组件直接 import 消费初始状态；
// 业务逻辑不在此层实现：realtime 随票 05、offlineQueue 状态机随票 06、selection/键位随票 07。
export { selectionStore, type CellRef, type SelectionState } from './selection'
export { editSessionStore, type EditSessionState } from './editSession'
export { presenceStore, type PresenceMember, type PresenceState } from './presence'
export { offlineQueueStore, type OfflineQueueItem, type OfflineQueueState, type ReplayProgress } from './offlineQueue'
export { viewConfigDraftStore, type ViewConfigDraftState } from './viewConfigDraft'
export { uiStore, type UiState } from './ui'
