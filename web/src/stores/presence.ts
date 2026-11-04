import { create } from 'zustand'

import type { CellRef } from './selection'

/** 在线协作者（专属色取 presence 色 10 色板索引，tokens.css --presence-color-N）。 */
export interface PresenceMember {
  readonly userId: string
  readonly colorIndex: number
}

/** presenceStore（技术方案第 8 章）：在线成员、专属色、他人光标/编辑描边。 */
export interface PresenceState {
  /** 在线成员与其专属色索引 */
  readonly onlineMembers: readonly PresenceMember[]
  /** 他人光标位置（userId → 单元格） */
  readonly otherCursors: Readonly<Record<string, CellRef>>
  /** 他人编辑中的单元格（编辑描边，userId → 单元格） */
  readonly editingBy: Readonly<Record<string, CellRef>>
}

/** 空骨架（票 02）：仅初始状态供任意组件消费；presence 事件分发随票 05 realtimeClient 落地。 */
export const presenceStore = create<PresenceState>()(() => ({
  onlineMembers: [],
  otherCursors: {},
  editingBy: {},
}))
