// 6 个 Zustand store（票 02 骨架，票 06 已转正 offlineQueue）：命名唯一事实源为《前端-技术方案-v0.1.md》第 8 章清单。
// 模块级单例，无需 Provider——任意组件直接 import 消费；
// 业务逻辑不在此层实现：realtime 随票 05、selection/键位随票 07。
export { authStore, type AuthSession, type AuthSessionInput, type AuthState } from './auth'
export { selectionStore, type CellRef, type SelectionState } from './selection'
export { editSessionStore, type EditSessionState } from './editSession'
export { presenceStore, type PresenceMember, type PresenceState } from './presence'
export {
  createOfflineQueueStore,
  offlineQueueStore,
  type CreateOfflineQueueStoreOptions,
  type OfflineQueueItem,
  type OfflineQueueState,
  type OfflineQueueStore,
  type ReplayProgress,
} from './offlineQueue'
export { viewConfigDraftStore, type ViewConfigDraftState } from './viewConfigDraft'
export {
  workspaceStore,
  deriveSaveState,
  trackWrite,
  useSaveState,
  type SaveState,
  type WorkspaceState,
} from './workspace'
export { uiStore, type UiState, type UndoWindow } from './ui'
