import { create } from 'zustand'

import type { CellRef } from './selection'

/** editSessionStore（技术方案第 8 章）：编辑中单元格、编辑前快照、version。 */
export interface EditSessionState {
  /** 正在编辑的单元格 */
  readonly editingCell: CellRef | null
  /** 进入编辑前的取值快照（Esc 取消时还原用） */
  readonly snapshotBeforeEdit: string | null
  /** 编辑会话携带的记录版本号（乐观锁比对） */
  readonly version: number
}

/** 空骨架（票 02）：仅初始状态供任意组件消费；编辑生命周期随票 07/08 落地。 */
export const editSessionStore = create<EditSessionState>()(() => ({
  editingCell: null,
  snapshotBeforeEdit: null,
  version: 0,
}))
