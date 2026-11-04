// SelectionModel 表驱动测试（票 07，技术方案 6.2「单元格/区域/整行三态」）。
// 纯函数语义：anchor（锚点）+ focus（焦点端）表达拖选；边界（空表/单格/越界钳制）为测试重点。
import { describe, expect, it } from 'vitest'

import type { CellRef, GridBounds } from './common'
import {
  selectAll,
  selectCell,
  selectionRect,
  selectRow,
  beginRange,
  extendRange,
  containsCell,
  cellCount,
} from './selection'

const BOUNDS: GridBounds = { rowCount: 5, colCount: 3 }

function cell(row: number, col: number): CellRef {
  return { row, col }
}

describe('selectCell（单元格选择）', () => {
  it('选中即 anchor=focus 的单格选择', () => {
    const selection = selectCell(cell(2, 1), BOUNDS)
    expect(selection?.kind).toBe('cell')
    expect(selection?.anchor).toEqual(cell(2, 1))
    expect(selection?.focus).toEqual(cell(2, 1))
  })

  it('越界坐标钳制到边界内', () => {
    const selection = selectCell(cell(99, 99), BOUNDS)
    expect(selection?.anchor).toEqual(cell(4, 2))
    expect(selection?.focus).toEqual(cell(4, 2))
  })

  it('空表（0 行或 0 列）选择为 null', () => {
    expect(selectCell(cell(0, 0), { rowCount: 0, colCount: 3 })).toBeNull()
    expect(selectCell(cell(0, 0), { rowCount: 5, colCount: 0 })).toBeNull()
  })
})

describe('区域选择（anchor + focus 拖选语义）', () => {
  it('beginRange 固定锚点；extendRange 只动 focus、anchor 不变（拖选）', () => {
    const started = beginRange(cell(1, 0), BOUNDS)
    expect(started?.kind).toBe('range')
    expect(started?.anchor).toEqual(cell(1, 0))
    const dragged = extendRange(started!, cell(3, 2), BOUNDS)!
    expect(dragged.kind).toBe('range')
    expect(dragged.anchor).toEqual(cell(1, 0)) // 锚点不动
    expect(dragged.focus).toEqual(cell(3, 2))
  })

  it('反向拖选（focus 在 anchor 左上）：rect 归一化为左上/右下界', () => {
    const started = beginRange(cell(3, 2), BOUNDS)
    const dragged = extendRange(started!, cell(1, 0), BOUNDS)!
    const rect = selectionRect(dragged)
    expect(rect).toEqual({ top: 1, left: 0, bottom: 3, right: 2 })
  })

  it('extendRange 越界钳制到网格边界；extendRange(null) 原样返回 null', () => {
    const started = beginRange(cell(4, 2), BOUNDS)
    const dragged = extendRange(started!, cell(99, -1), BOUNDS)!
    expect(dragged.focus).toEqual(cell(4, 0)) // 钳制到边界（col -1 → 0，row 99 → 4）
    expect(dragged.anchor).toEqual(cell(4, 2)) // 锚点不受钳制影响
    expect(extendRange(null, cell(0, 0), BOUNDS)).toBeNull()
  })

  it('containsCell 按归一化矩形判断；单格选择即等点判断', () => {
    const started = beginRange(cell(1, 1), BOUNDS)
    const dragged = extendRange(started!, cell(3, 2), BOUNDS)
    expect(containsCell(dragged, cell(2, 2))).toBe(true)
    expect(containsCell(dragged, cell(4, 0))).toBe(false)
    const single = selectCell(cell(2, 1), BOUNDS)
    expect(containsCell(single, cell(2, 1))).toBe(true)
    expect(containsCell(single, cell(2, 2))).toBe(false)
  })

  it('cellCount：区域 = 行数 × 列数；单格 = 1', () => {
    const started = beginRange(cell(1, 1), BOUNDS)
    const dragged = extendRange(started!, cell(3, 2), BOUNDS)
    expect(cellCount(dragged)).toBe(6)
    expect(cellCount(selectCell(cell(0, 0), BOUNDS))).toBe(1)
  })
})

describe('selectRow（整行选择）', () => {
  it('整行 = 该行首列到末列的区域', () => {
    const selection = selectRow(2, BOUNDS)
    expect(selection?.kind).toBe('row')
    expect(selection?.anchor).toEqual(cell(2, 0))
    expect(selection?.focus).toEqual(cell(2, 2))
    expect(cellCount(selection)).toBe(3)
    expect(containsCell(selection, cell(2, 1))).toBe(true)
    expect(containsCell(selection, cell(3, 1))).toBe(false)
  })

  it('行号越界钳制；空表为 null', () => {
    expect(selectRow(99, BOUNDS)?.anchor).toEqual(cell(4, 0))
    expect(selectRow(0, { rowCount: 0, colCount: 3 })).toBeNull()
  })
})

describe('selectAll（全选边界）', () => {
  it('全选 = 左上到右下整表区域', () => {
    const selection = selectAll(BOUNDS)
    expect(selection?.kind).toBe('all')
    expect(selection?.anchor).toEqual(cell(0, 0))
    expect(selection?.focus).toEqual(cell(4, 2))
    expect(cellCount(selection)).toBe(15)
  })

  it('空表全选为 null（边界）', () => {
    expect(selectAll({ rowCount: 0, colCount: 3 })).toBeNull()
    expect(selectAll({ rowCount: 5, colCount: 0 })).toBeNull()
  })

  it('单格表全选 = 唯一格（kind 仍为 all）', () => {
    const selection = selectAll({ rowCount: 1, colCount: 1 })
    expect(selection?.kind).toBe('all')
    expect(selection?.anchor).toEqual(cell(0, 0))
    expect(selection?.focus).toEqual(cell(0, 0))
    expect(cellCount(selection)).toBe(1)
  })
})
