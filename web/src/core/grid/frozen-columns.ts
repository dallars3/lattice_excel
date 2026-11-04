// 冻结列双通道布局（票 07，技术方案 6.1「冻结列与非冻结列双通道渲染（sticky 定位实现左固定）；横向滚动复用同一虚拟化引擎」）。
// DOM 无关纯函数：输入列宽表/冻结列数/横向偏移，输出固定区与滚动区的列集合划分 + 滚动区横向虚拟化窗口。
// 渲染层（票 09）把固定区按 sticky 定位、滚动区按本窗口平移渲染；本模块只做几何划分。

import { computeWindow, DEFAULT_OVERSCAN } from './windowing'

export interface ComputeColumnLayoutInput {
  /** 全表列宽表（索引即列号，含冻结列与滚动列）。 */
  readonly columnWidths: readonly number[]
  /** 冻结列数（取最左连续列）；≥ 总列数时全部冻结；负值钳制为 0。 */
  readonly frozenCount: number
  /** 横向滚动偏移（滚动区通道内，scrollLeft）。 */
  readonly scrollLeft: number
  /** 整个网格视口宽（含固定区占用）。 */
  readonly viewportWidth: number
  /** 滚动区横向 overscan 列数；缺省与纵向一致（上下各 5 → 左右各 5）。 */
  readonly overscan?: number
}

/** 通道内一个已定位列：全表列号 + 通道内偏移 + 列宽。 */
export interface PositionedColumn {
  /** 全表列号（0 起）。 */
  readonly index: number
  /** 通道内起点偏移：固定区相对内容原点；滚动区相对滚动区内容原点（平移渲染用）。 */
  readonly offset: number
  readonly width: number
}

export interface ColumnLayout {
  /** 固定区列集合（sticky 通道，完整输出不虚拟化）。 */
  readonly frozen: readonly PositionedColumn[]
  /** 滚动区可见列窗口（横向虚拟化，含 overscan）。 */
  readonly scrollable: readonly PositionedColumn[]
  /** 实际生效冻结列数（钳制后）。 */
  readonly frozenCount: number
  /** 固定区总宽。 */
  readonly frozenWidth: number
  /** 滚动区视口宽 = viewportWidth - frozenWidth（被固定区占满时为 0）。 */
  readonly scrollViewportWidth: number
  /** 滚动区窗口首个全表列号；滚动区空窗口为 -1。 */
  readonly scrollStartIndex: number
  /** 滚动区窗口末个全表列号；滚动区空窗口为 -1。 */
  readonly scrollEndIndex: number
  /** 全表总宽。 */
  readonly totalWidth: number
}

/**
 * 冻结列双通道划分 + 滚动区横向虚拟化窗口。
 * 滚动区窗口复用纵向同一引擎（computeWindow）：滚动区列宽表切出后按横向偏移切窗，
 * 全表列号 = frozenCount + 滚动区局部索引。
 */
export function computeColumnLayout(input: ComputeColumnLayoutInput): ColumnLayout {
  const { columnWidths, scrollLeft, viewportWidth } = input
  const frozenCount = Math.min(Math.max(0, Math.trunc(input.frozenCount)), columnWidths.length)
  const overscan = input.overscan ?? DEFAULT_OVERSCAN

  // 固定区通道：最左 frozenCount 列，完整输出（sticky 定位由渲染层实现）
  const frozen: PositionedColumn[] = []
  let frozenWidth = 0
  for (let i = 0; i < frozenCount; i += 1) {
    frozen.push({ index: i, offset: frozenWidth, width: columnWidths[i] })
    frozenWidth += columnWidths[i]
  }

  const totalWidth = columnWidths.reduce((a, b) => a + b, 0)
  const scrollViewportWidth = Math.max(0, viewportWidth - frozenWidth)
  const scrollSizes = columnWidths.slice(frozenCount)
  const window = computeWindow({
    sizes: scrollSizes,
    scrollOffset: scrollLeft,
    viewportSize: scrollViewportWidth,
    overscan,
  })

  const scrollable: PositionedColumn[] = window.sections.map((section) => ({
    index: frozenCount + section.index,
    offset: section.offset,
    width: section.size,
  }))

  return {
    frozen,
    scrollable,
    frozenCount,
    frozenWidth,
    scrollViewportWidth,
    scrollStartIndex: window.start === -1 ? -1 : frozenCount + window.start,
    scrollEndIndex: window.end === -1 ? -1 : frozenCount + window.end,
    totalWidth,
  }
}
