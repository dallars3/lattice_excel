import { create } from 'zustand'

/** 删除撤销窗口（H-54）：删除成功后 7 秒内可撤销；跨页面切换不重置（仅按 deadline 计时）。 */
export interface UndoWindow {
  readonly baseId: string
  readonly recordIds: readonly string[]
  readonly deadlineAt: number
}

/** uiStore（技术方案第 8 章）：横幅/Toast/Modal 栈、删除撤销 7s 计时器。 */
export interface UiState {
  /** 全局横幅文案（非空时渲染） */
  readonly bannerMessage: string | null
  /** Modal 栈（栈顶最后入栈，后到先关） */
  readonly modalStack: readonly string[]
  /** 删除撤销窗口截止时间戳（ms；null 表示无撤销窗口） */
  readonly undoDeadlineAt: number | null
  /** 最近一次删除撤销窗口（ids + baseId + deadline）。 */
  readonly undoWindow: UndoWindow | null
  setUndoWindow: (window: UndoWindow) => void
  clearUndoWindow: () => void
}

export const uiStore = create<UiState>()((set) => ({
  bannerMessage: null,
  modalStack: [],
  undoDeadlineAt: null,
  undoWindow: null,
  setUndoWindow: (window) =>
    set({ undoWindow: window, undoDeadlineAt: window.deadlineAt }),
  clearUndoWindow: () => set({ undoWindow: null, undoDeadlineAt: null }),
}))
