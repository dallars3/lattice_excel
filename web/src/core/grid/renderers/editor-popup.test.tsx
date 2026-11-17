// 弹层 portal（票 01 / BUG-06）单元测试：定位纯函数 resolvePopupPlacement 的
// 默认下方、视口边缘翻转、水平收拢契约。几何期望值为手算字面量（独立事实源）。
import { describe, expect, it } from 'vitest'

import { resolvePopupPlacement } from './editor-popup'

describe('resolvePopupPlacement', () => {
  it('下方空间充足：默认锚下方、与锚左对齐', () => {
    const placement = resolvePopupPlacement(
      { top: 100, bottom: 130, left: 40 },
      { width: 220, height: 179 },
      { width: 1024, height: 768 },
    )

    expect(placement).toEqual({ top: 132, left: 40, flipped: false })
  })

  it('下方放不下且上方空间更大：翻转锚上方', () => {
    // spaceBelow = 768-730-2 = 36 < 179，spaceAbove = 700-2 = 698 > 36
    const placement = resolvePopupPlacement(
      { top: 700, bottom: 730, left: 40 },
      { width: 220, height: 179 },
      { width: 1024, height: 768 },
    )

    expect(placement).toEqual({ top: 519, left: 40, flipped: true })
  })

  it('上下都放不下：保持下方（贴着锚，best effort）不翻转', () => {
    // spaceBelow = 500-330-2 = 168 < 179，spaceAbove = 50-2 = 48 ≤ 168
    const placement = resolvePopupPlacement(
      { top: 50, bottom: 330, left: 40 },
      { width: 220, height: 179 },
      { width: 1024, height: 500 },
    )

    expect(placement).toEqual({ top: 332, left: 40, flipped: false })
  })

  it('左对齐后右溢出视口：右缘收回（预留 8px 边距）', () => {
    const placement = resolvePopupPlacement(
      { top: 100, bottom: 120, left: 900 },
      { width: 220, height: 179 },
      { width: 1024, height: 768 },
    )

    expect(placement).toEqual({ top: 122, left: 796, flipped: false })
  })

  it('右缘收拢会越出左缘时：夹到视口左边距', () => {
    const placement = resolvePopupPlacement(
      { top: 100, bottom: 120, left: 2 },
      { width: 220, height: 179 },
      { width: 100, height: 768 },
    )

    expect(placement.left).toBe(8)
  })

  it('翻转后弹层高过视口上界：top 不小于 8px 边距', () => {
    const placement = resolvePopupPlacement(
      { top: 100, bottom: 700, left: 40 },
      { width: 220, height: 700 },
      { width: 1024, height: 768 },
    )

    expect(placement.flipped).toBe(true)
    expect(placement.top).toBe(8)
  })
})
