// SelectionModel（票 07，技术方案 6.2「单元格/区域/整行三态」）。
// 纯函数语义：anchor（锚点，拖选起点）+ focus（焦点端）表达选择；
// 区域 = 归一化矩形 [anchor, focus]；越界一律钳制、空表选择为 null（显式，不抛错）。
// 技术方案 6.2「与 cursor_echo 冲突时本地选择优先」是消费层（票 09/M2）职责，本模块只管几何。

import { clampCell, type CellRef, type GridBounds } from './common'

/** 选择三态（技术方案 6.2）+ 全选（区域满表的特例，便于消费层区分语义）。 */
export const SELECTION_KINDS = ['cell', 'range', 'row', 'all'] as const
export type SelectionKind = (typeof SELECTION_KINDS)[number]

export interface Selection {
  readonly kind: SelectionKind
  /** 锚点（拖选起点；整行=该行首列；全选=左上角）。 */
  readonly anchor: CellRef
  /** 焦点端（拖选移动端；整行=该行末列；全选=右下角）。 */
  readonly focus: CellRef
}

/** 归一化选择矩形（左上/右下闭区间）。 */
export interface SelectionRect {
  readonly top: number
  readonly left: number
  readonly bottom: number
  readonly right: number
}

/** 空表下的选择：null（显式无选择）。 */
export type SelectionOrNull = Selection | null

/** 单元格选择：anchor = focus = 钳制后坐标。 */
export function selectCell(cell: CellRef, bounds: GridBounds): SelectionOrNull {
  if (bounds.rowCount <= 0 || bounds.colCount <= 0) {
    return null
  }
  const clamped = clampCell(cell, bounds)
  return { kind: 'cell', anchor: clamped, focus: clamped }
}

/** 拖选起点：固定锚点。 */
export function beginRange(cell: CellRef, bounds: GridBounds): SelectionOrNull {
  if (bounds.rowCount <= 0 || bounds.colCount <= 0) {
    return null
  }
  const clamped = clampCell(cell, bounds)
  return { kind: 'range', anchor: clamped, focus: clamped }
}

/** 拖选移动：锚点不动，焦点端更新（拖选语义）。kind 归一为 range。 */
export function extendRange(selection: SelectionOrNull, cell: CellRef, bounds: GridBounds): SelectionOrNull {
  if (selection === null || bounds.rowCount <= 0 || bounds.colCount <= 0) {
    return selection
  }
  return { kind: 'range', anchor: selection.anchor, focus: clampCell(cell, bounds) }
}

/** 整行选择：该行首列到末列。 */
export function selectRow(row: number, bounds: GridBounds): SelectionOrNull {
  if (bounds.rowCount <= 0 || bounds.colCount <= 0) {
    return null
  }
  const clampedRow = Math.min(Math.max(0, row), bounds.rowCount - 1)
  return { kind: 'row', anchor: { row: clampedRow, col: 0 }, focus: { row: clampedRow, col: bounds.colCount - 1 } }
}

/** 全选：左上到右下整表；空表为 null。 */
export function selectAll(bounds: GridBounds): SelectionOrNull {
  if (bounds.rowCount <= 0 || bounds.colCount <= 0) {
    return null
  }
  return { kind: 'all', anchor: { row: 0, col: 0 }, focus: { row: bounds.rowCount - 1, col: bounds.colCount - 1 } }
}

/** 归一化矩形（任意方向拖选都得到左上/右下界）。 */
export function selectionRect(selection: SelectionOrNull): SelectionRect {
  if (selection === null) {
    return { top: 0, left: 0, bottom: -1, right: -1 } // 空矩形：contains 恒 false
  }
  return {
    top: Math.min(selection.anchor.row, selection.focus.row),
    left: Math.min(selection.anchor.col, selection.focus.col),
    bottom: Math.max(selection.anchor.row, selection.focus.row),
    right: Math.max(selection.anchor.col, selection.focus.col),
  }
}

/** 单元格是否落在选择矩形内。 */
export function containsCell(selection: SelectionOrNull, cell: CellRef): boolean {
  if (selection === null) {
    return false
  }
  const rect = selectionRect(selection)
  return cell.row >= rect.top && cell.row <= rect.bottom && cell.col >= rect.left && cell.col <= rect.right
}

/** 选择覆盖的单元格数量。 */
export function cellCount(selection: SelectionOrNull): number {
  if (selection === null) {
    return 0
  }
  const rect = selectionRect(selection)
  if (rect.bottom < rect.top || rect.right < rect.left) {
    return 0
  }
  return (rect.bottom - rect.top + 1) * (rect.right - rect.left + 1)
}
