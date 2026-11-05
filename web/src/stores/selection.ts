import { create } from 'zustand'

import type { CellRef } from '../core/grid/common'

/**
 * 单元格坐标引用：单一事实源在 core/grid/common.ts（票 07 落地时已收敛）。
 * 此处 re-export 保持 stores 层既有导入路径（./selection、@/stores）不破坏。
 */
export type { CellRef }

/** selectionStore（技术方案第 8 章）：选择模型、键盘焦点。 */
export interface SelectionState {
  /** 键盘焦点所在单元格 */
  readonly focusedCell: CellRef | null
  /** 当前选中区域（含单选），以左上/右下界表示 */
  readonly selectedRange: { readonly start: CellRef; readonly end: CellRef } | null
}

/** 空骨架（票 02）：仅初始状态供任意组件消费；选择/键位业务动作随票 07 落地。 */
export const selectionStore = create<SelectionState>()(() => ({
  focusedCell: null,
  selectedRange: null,
}))
