// grid 公共类型与小工具（票 07）。DOM 无关，术语沿用 GLOSSARY.md（单元格 Cell = 记录 × 字段交点）。

/** 单元格坐标：row = 记录行号，col = 字段列号（均 0 起）。 */
export interface CellRef {
  readonly row: number
  readonly col: number
}

/** 网格边界：行数（记录数）与列数（可见字段数）。 */
export interface GridBounds {
  readonly rowCount: number
  readonly colCount: number
}

/** 坐标钳制到边界内（空边界返回原值，调用方先判空）。 */
export function clampCell(cell: CellRef, bounds: GridBounds): CellRef {
  return {
    row: Math.min(Math.max(0, cell.row), Math.max(0, bounds.rowCount - 1)),
    col: Math.min(Math.max(0, cell.col), Math.max(0, bounds.colCount - 1)),
  }
}
