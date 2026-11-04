// 冻结列双通道表驱动测试（票 07，技术方案 6.1「冻结列与非冻结列双通道渲染；横向滚动复用同一虚拟化引擎」）。
// 期望值全部为手算字面量。
import { describe, expect, it } from 'vitest'

import { computeColumnLayout } from './frozen-columns'

// 列宽表（8 列，总宽 980）：冻结 2 列后滚动区尺寸表 [100,150,90,200,60,180]（总宽 780）
const WIDTHS = [80, 120, 100, 150, 90, 200, 60, 180] as const

describe('computeColumnLayout', () => {
  it('双通道划分：固定区取前 frozenCount 列（sticky 原点偏移），滚动区从冻结列之后开始', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 2, scrollLeft: 0, viewportWidth: 500, overscan: 0 })
    expect(layout.frozenCount).toBe(2)
    expect(layout.frozen.map((c) => c.index)).toEqual([0, 1])
    expect(layout.frozen.map((c) => c.offset)).toEqual([0, 80])
    expect(layout.frozen.map((c) => c.width)).toEqual([80, 120])
    expect(layout.frozenWidth).toBe(200)
    expect(layout.totalWidth).toBe(980)
  })

  it('滚动区横向窗口：与纵向同一引擎语义（视口 = viewportWidth - frozenWidth）', () => {
    // 滚动区视口 300；scrollLeft 0 → 滚动区内容节 0..2（100+150+90=340 ≥ 300）
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 2, scrollLeft: 0, viewportWidth: 500, overscan: 0 })
    expect(layout.scrollViewportWidth).toBe(300)
    expect(layout.scrollStartIndex).toBe(2)
    expect(layout.scrollEndIndex).toBe(4)
    expect(layout.scrollable.map((c) => c.index)).toEqual([2, 3, 4])
    expect(layout.scrollable.map((c) => c.offset)).toEqual([0, 100, 250])
  })

  it('横向滚动：scrollLeft 100 → 窗口右移一列', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 2, scrollLeft: 100, viewportWidth: 500, overscan: 0 })
    expect(layout.scrollStartIndex).toBe(3)
    expect(layout.scrollEndIndex).toBe(5)
    expect(layout.scrollable.map((c) => c.index)).toEqual([3, 4, 5])
  })

  it('横向滚动中段：scrollLeft 400 → 内容列 5..7', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 2, scrollLeft: 400, viewportWidth: 500, overscan: 0 })
    expect(layout.scrollable.map((c) => c.index)).toEqual([5, 6, 7])
    expect(layout.scrollable.map((c) => c.offset)).toEqual([340, 540, 600])
  })

  it('横向 overscan（上下/左右各 5）：中段窗口向两侧扩展并钳制在滚动区内', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 2, scrollLeft: 400, viewportWidth: 500 })
    // 内容列 5..7，overscan 左扩 5 钳到滚动区首（全局 2），右扩 5 钳到末列 7
    expect(layout.scrollStartIndex).toBe(2)
    expect(layout.scrollEndIndex).toBe(7)
    expect(layout.scrollable.map((c) => c.index)).toEqual([2, 3, 4, 5, 6, 7])
  })

  it('横向越界钳制：scrollLeft 超总宽输出滚动区末尾窗口', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 2, scrollLeft: 99999, viewportWidth: 500, overscan: 0 })
    expect(layout.scrollable.map((c) => c.index)).toEqual([5, 6, 7])
  })

  it('冻结列数为 0：无固定区，滚动区覆盖全表视口（80+120+100+150+90=540 ≥ 500 → 列 0..4）', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 0, scrollLeft: 0, viewportWidth: 500, overscan: 0 })
    expect(layout.frozen).toEqual([])
    expect(layout.frozenWidth).toBe(0)
    expect(layout.scrollStartIndex).toBe(0)
    expect(layout.scrollable.map((c) => c.index)).toEqual([0, 1, 2, 3, 4])
  })

  it('冻结列数 ≥ 总列数：全部列进固定区，滚动区为空窗口', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 8, scrollLeft: 0, viewportWidth: 500 })
    expect(layout.frozenCount).toBe(8)
    expect(layout.frozen.map((c) => c.index)).toEqual([0, 1, 2, 3, 4, 5, 6, 7])
    expect(layout.frozenWidth).toBe(980)
    expect(layout.scrollable).toEqual([])
    expect(layout.scrollStartIndex).toBe(-1)
    expect(layout.scrollEndIndex).toBe(-1)
  })

  it('负冻结列数钳制为 0（不抛错，显式归一）', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: -3, scrollLeft: 0, viewportWidth: 500, overscan: 0 })
    expect(layout.frozenCount).toBe(0)
    expect(layout.frozen).toEqual([])
    expect(layout.scrollStartIndex).toBe(0)
  })

  it('视口被固定区占满（viewportWidth < frozenWidth）：滚动区视口为 0，滚动区为空窗口', () => {
    const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount: 2, scrollLeft: 0, viewportWidth: 100 })
    expect(layout.scrollViewportWidth).toBe(0)
    expect(layout.scrollable).toEqual([])
    expect(layout.scrollStartIndex).toBe(-1)
    // 固定区照常完整输出
    expect(layout.frozen.map((c) => c.index)).toEqual([0, 1])
  })

  it('空列宽表：双通道均为空，总宽 0', () => {
    const layout = computeColumnLayout({ columnWidths: [], frozenCount: 2, scrollLeft: 0, viewportWidth: 500 })
    expect(layout.frozen).toEqual([])
    expect(layout.scrollable).toEqual([])
    expect(layout.frozenWidth).toBe(0)
    expect(layout.totalWidth).toBe(0)
    expect(layout.scrollStartIndex).toBe(-1)
  })

  it('不变量：固定区索引恰为 [0, frozenCount)，滚动区索引连续且紧接其后（不跳列不重复）', () => {
    for (const frozenCount of [0, 1, 2, 3, 5]) {
      for (const scrollLeft of [0, 120, 400, 780, 99999]) {
        const layout = computeColumnLayout({ columnWidths: WIDTHS, frozenCount, scrollLeft, viewportWidth: 300 })
        expect(layout.frozen.map((c) => c.index)).toEqual(
          Array.from({ length: layout.frozenCount }, (_, i) => i),
        )
        if (layout.scrollable.length > 0) {
          for (let i = 0; i < layout.scrollable.length; i += 1) {
            expect(layout.scrollable[i].index).toBe(layout.scrollStartIndex + i)
          }
          expect(layout.scrollStartIndex).toBeGreaterThanOrEqual(layout.frozenCount)
          expect(layout.scrollEndIndex).toBeLessThanOrEqual(WIDTHS.length - 1)
        } else {
          expect(layout.scrollStartIndex).toBe(-1)
          expect(layout.scrollEndIndex).toBe(-1)
        }
      }
    }
  })
})
