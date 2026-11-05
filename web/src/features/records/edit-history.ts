// 会话内编辑撤销栈（票 06 / AC-REC-07）：模块级 Zustand store，不落 localStorage；
// 刷新后模块重建即不可撤销。仅记录成功落库的单元格编辑（before/after/提交前版本）。
import { create } from 'zustand'

export interface EditHistoryEntry {
  readonly id: number
  readonly baseId: string
  readonly recordId: string
  readonly fieldId: string
  readonly before: unknown
  readonly after: unknown
  /** 提交前记录版本（撤销时用当前版本，不用此值；仅作诊断）。 */
  readonly versionAtEdit: number
}

export interface EditHistoryState {
  readonly entries: readonly EditHistoryEntry[]
  push: (entry: Omit<EditHistoryEntry, 'id'>) => void
  /** 最近一步（同 Base）不弹出，供撤销成功后显式 drop。 */
  peek: (baseId: string) => EditHistoryEntry | null
  drop: (id: number) => void
  clear: () => void
}

let nextId = 1

/** 模块级单例：页面刷新（模块重建）后清空，满足「刷新后不可撤销」。 */
export const editHistoryStore = create<EditHistoryState>()((set, get) => ({
  entries: [],
  push: (entry) =>
    set((state) => ({
      entries: [...state.entries, { ...entry, id: nextId++ }],
    })),
  peek: (baseId) => {
    const entries = get().entries
    for (let index = entries.length - 1; index >= 0; index -= 1) {
      const entry = entries[index]
      if (entry !== undefined && entry.baseId === baseId) return entry
    }
    return null
  },
  drop: (id) =>
    set((state) => ({ entries: state.entries.filter((entry) => entry.id !== id) })),
  clear: () => set({ entries: [] }),
}))
