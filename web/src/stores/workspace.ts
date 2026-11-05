import { create } from 'zustand'

/** 顶栏保存状态三态（票 03）：在途写计数 > 0 为 saving；offline 占位优先。 */
export type SaveState = 'saving' | 'saved' | 'offline'

/** 附件显示元数据（票 08）：file_id → 文件名/显示名，供抽屉与网格即时显示上传结果。 */
export interface FileMetaSummary {
  readonly name: string
  readonly displayName: string
}

/** workspaceStore：瞬时 UI 态（不存服务端数据）；写动作统一经 trackWrite 计入在途计数。 */
export interface WorkspaceState {
  readonly inFlightWriteCount: number
  /** 离线占位（票 09 接线；本票只呈现状态，不排队重放）。 */
  readonly offline: boolean
  /**
   * 会话内字段描述映射（fieldId → description）：H-35 网关不接受 description，
   * 新建字段时先写本地，表头悬停优先取本地值（票 04 G 项）。
   */
  readonly fieldDescriptions: Record<string, string>
  /** 会话内附件显示映射（file_id → 文件名）：上传/重命名/删除后同步网格与抽屉。 */
  readonly fileMetaMap: Record<string, FileMetaSummary>
  beginWrite: () => void
  endWrite: () => void
  setOffline: (offline: boolean) => void
  setFieldDescription: (fieldId: string, description: string) => void
  clearFieldDescriptions: () => void
  setFileMeta: (fileId: string, meta: FileMetaSummary) => void
  removeFileMeta: (fileId: string) => void
}

export const workspaceStore = create<WorkspaceState>()((set) => ({
  inFlightWriteCount: 0,
  offline: false,
  fieldDescriptions: {},
  fileMetaMap: {},
  beginWrite: () => set((state) => ({ inFlightWriteCount: state.inFlightWriteCount + 1 })),
  endWrite: () =>
    set((state) => ({ inFlightWriteCount: Math.max(0, state.inFlightWriteCount - 1) })),
  setOffline: (offline) => set({ offline }),
  setFieldDescription: (fieldId, description) =>
    set((state) => ({ fieldDescriptions: { ...state.fieldDescriptions, [fieldId]: description } })),
  clearFieldDescriptions: () => set({ fieldDescriptions: {} }),
  setFileMeta: (fileId, meta) =>
    set((state) => ({ fileMetaMap: { ...state.fileMetaMap, [fileId]: meta } })),
  removeFileMeta: (fileId) =>
    set((state) => {
      if (!(fileId in state.fileMetaMap)) return state
      const next = { ...state.fileMetaMap }
      delete next[fileId]
      return { fileMetaMap: next }
    }),
}))

/** 保存状态派生（纯函数，供组件与测试复用）。 */
export function deriveSaveState(inFlightWriteCount: number, offline: boolean): SaveState {
  if (offline) return 'offline'
  return inFlightWriteCount > 0 ? 'saving' : 'saved'
}

/** 组件侧保存状态订阅：计数或离线位变化时重渲染。 */
export function useSaveState(): SaveState {
  return workspaceStore((state) => deriveSaveState(state.inFlightWriteCount, state.offline))
}

/** 包裹一次写请求：进入即计数 +1，成功/失败均在 finally 归零。 */
export async function trackWrite<T>(task: () => Promise<T>): Promise<T> {
  workspaceStore.getState().beginWrite()
  try {
    return await task()
  } finally {
    workspaceStore.getState().endWrite()
  }
}
