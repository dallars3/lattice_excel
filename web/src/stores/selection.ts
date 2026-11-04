import { create } from 'zustand'

/** 单元格坐标引用（骨架最小形状；票 07 grid core 落 SelectionModel 时收敛为 core/grid 类型）。 */
export interface CellRef {
  readonly row: number
  readonly col: number
}

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
