import { create } from 'zustand'

/** 顶栏保存状态三态（票 03）：在途写计数 > 0 为 saving；offline 占位优先。 */
export type SaveState = 'saving' | 'saved' | 'offline'

/** workspaceStore：瞬时 UI 态（不存服务端数据）；写动作统一经 trackWrite 计入在途计数。 */
export interface WorkspaceState {
  readonly inFlightWriteCount: number
  /** 离线占位（票 09 接线；本票只呈现状态，不排队重放）。 */
  readonly offline: boolean
  beginWrite: () => void
  endWrite: () => void
  setOffline: (offline: boolean) => void
}

export const workspaceStore = create<WorkspaceState>()((set) => ({
  inFlightWriteCount: 0,
  offline: false,
  beginWrite: () => set((state) => ({ inFlightWriteCount: state.inFlightWriteCount + 1 })),
  endWrite: () =>
    set((state) => ({ inFlightWriteCount: Math.max(0, state.inFlightWriteCount - 1) })),
  setOffline: (offline) => set({ offline }),
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
