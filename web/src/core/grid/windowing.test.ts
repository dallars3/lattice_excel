// 虚拟滚动窗口计算表驱动测试（票 07，技术方案 6.1 自研虚拟滚动 / spec 好测试标准）。
// 事实源：技术方案 6.1——overscan 上下各 5 行；期望值全部为手算字面量（独立事实源，非复算）。
import { describe, expect, it } from 'vitest'

import {
  DEFAULT_OVERSCAN,
  FRAME_BUDGET_MS,
  computeWindow,
  type WindowResult,
} from './windowing'

// ---------- 断言工具 ----------

/** 窗口结构不变量：索引连续不跳行不重复、首末节与视口相交关系成立。 */
function expectWindowInvariants(result: WindowResult, sizes: readonly number[], scrollOffset: number, viewportSize: number): void {
  const { start, end, sections, totalSize } = result
  if (sections.length === 0) {
    expect(start).toBe(-1)
    expect(end).toBe(-1)
    return
  }
  // 不跳行不重复：索引严格连续递增
  for (let i = 0; i < sections.length; i += 1) {
    expect(sections[i].index).toBe(start + i)
  }
  // 索引落在合法范围
  expect(start).toBeGreaterThanOrEqual(0)
  expect(end).toBeLessThanOrEqual(sizes.length - 1)
  expect(end).toBeGreaterThanOrEqual(start)
  // 每节 size 与源表一致、offset 为前缀和
  let offset = 0
  for (let i = 0; i < start; i += 1) offset += sizes[i]
  for (const section of sections) {
    expect(section.size).toBe(sizes[section.index])
    expect(section.offset).toBe(offset)
    offset += sizes[section.index]
  }
  // 总高独立复算（全表求和，非前缀推进值）
  expect(totalSize).toBe(sizes.reduce((a, b) => a + b, 0))
  // 末节底沿必须到达视口底沿（或已是最后一节）
  const last = sections[sections.length - 1]
  if (end < sizes.length - 1) {
    expect(last.offset + last.size).toBeGreaterThanOrEqual(scrollOffset + viewportSize)
  }
  expect(sections[0].offset).toBeLessThanOrEqual(scrollOffset)
}

// ---------- 用例 ----------

describe('computeWindow', () => {
  it('帧预算常量为 16ms（技术方案 6.1；消费策略后置，M1 不做降级实现）', () => {
    expect(FRAME_BUDGET_MS).toBe(16)
  })

  it('默认 overscan 为上下各 5', () => {
    expect(DEFAULT_OVERSCAN).toBe(5)
  })

  it('空数据窗口：无任何可见节', () => {
    const result = computeWindow({ sizes: [], scrollOffset: 0, viewportSize: 100 })
    expect(result.sections).toEqual([])
    expect(result.start).toBe(-1)
    expect(result.end).toBe(-1)
    expect(result.totalSize).toBe(0)
  })

  // 表驱动：期望值为手算字面量
  // 均匀行高 40px：索引 i 的区间为 [40i, 40i+40)
  const cases: ReadonlyArray<{
    readonly name: string
    readonly sizes: readonly number[]
    readonly scrollOffset: number
    readonly viewportSize: number
    readonly overscan?: number
    readonly expectedStart: number
    readonly expectedEnd: number
    readonly expectedContentStart: number
    readonly expectedContentEnd: number
  }> = [
    {
      name: '顶部：offset 0 从首行开始，overscan 只向下展开',
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: 0,
      viewportSize: 100, // 可见行 0..2（0-100px）
      expectedStart: 0,
      expectedEnd: 7, // 内容末 2 + overscan 5
      expectedContentStart: 0,
      expectedContentEnd: 2,
    },
    {
      name: '中部：offset 400 落在行 10 起点，上下 overscan 完整展开',
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: 400,
      viewportSize: 100, // 可见行 10..12
      expectedStart: 5,
      expectedEnd: 17,
      expectedContentStart: 10,
      expectedContentEnd: 12,
    },
    {
      name: '底部：offset 1900（末行贴底），overscan 只向上展开',
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: 1900, // 总高 2000，视口 100
      viewportSize: 100, // 可见行 47..49
      expectedStart: 42,
      expectedEnd: 49,
      expectedContentStart: 47,
      expectedContentEnd: 49,
    },
    {
      name: '分组行混合：分组行高 80 与普通行 40 交错，按实际前缀和切窗',
      // 行 0/1 普通 40，行 2 分组 80，行 3/4 普通 40 → 前缀 [0,40,80,160,200,240]
      sizes: [40, 40, 80, 40, 40],
      scrollOffset: 80,
      viewportSize: 100, // 80..180 与行 2（80-160）、行 3（160-200）相交
      expectedStart: 0, // 内容首 2 - overscan 5 钳到 0
      expectedEnd: 4, // 内容末 3 + overscan 5 钳到 4
      expectedContentStart: 2,
      expectedContentEnd: 3,
    },
    {
      name: '分组行混合：滚动到分组行中段，窗口按偏移精确定位',
      sizes: [40, 40, 80, 40, 40],
      scrollOffset: 100, // 落在分组行 2（80-160）内部
      viewportSize: 60, // 100..160 仅与行 2 相交
      expectedStart: 0,
      expectedEnd: 4,
      expectedContentStart: 2,
      expectedContentEnd: 2,
    },
    {
      name: '单行高于视口：窗口仍含该行',
      sizes: [500],
      scrollOffset: 120,
      viewportSize: 100,
      expectedStart: 0,
      expectedEnd: 0,
      expectedContentStart: 0,
      expectedContentEnd: 0,
    },
    {
      name: '滚动越界钳制：offset 超过总高时输出末尾窗口',
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: 99999,
      viewportSize: 100,
      expectedStart: 42,
      expectedEnd: 49,
      expectedContentStart: 47,
      expectedContentEnd: 49,
    },
    {
      name: '负偏移钳制为 0（顶部窗口）',
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: -50,
      viewportSize: 100,
      expectedStart: 0,
      expectedEnd: 7,
      expectedContentStart: 0,
      expectedContentEnd: 2,
    },
    {
      name: 'overscan=0：窗口即内容窗口（精确可见集）',
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: 400,
      viewportSize: 100,
      overscan: 0,
      expectedStart: 10,
      expectedEnd: 12,
      expectedContentStart: 10,
      expectedContentEnd: 12,
    },
    {
      name: '视口为 0：输出空窗口',
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: 0,
      viewportSize: 0,
      expectedStart: -1,
      expectedEnd: -1,
      expectedContentStart: -1,
      expectedContentEnd: -1,
    },
  ]

  for (const c of cases) {
    it(`${c.name}`, () => {
      const result = computeWindow({
        sizes: c.sizes,
        scrollOffset: c.scrollOffset,
        viewportSize: c.viewportSize,
        ...(c.overscan !== undefined ? { overscan: c.overscan } : {}),
      })
      expect(result.start).toBe(c.expectedStart)
      expect(result.end).toBe(c.expectedEnd)
      expect(result.contentStart).toBe(c.expectedContentStart)
      expect(result.contentEnd).toBe(c.expectedContentEnd)
      if (c.expectedStart === -1) {
        expect(result.sections).toEqual([])
      } else {
        expect(result.sections.map((s) => s.index)).toEqual(
          Array.from({ length: c.expectedEnd - c.expectedStart + 1 }, (_, i) => c.expectedStart + i),
        )
      }
      expectWindowInvariants(result, c.sizes, Math.max(0, c.scrollOffset), c.viewportSize)
    })
  }

  it('可见集合语义：给定滚动偏移输出恰好与视口相交的行集合（含边界行）', () => {
    // 行区间 [40i, 40i+40) 与视口 [150, 250) 相交的是行 3(120-160)、4、5、6(240-280)
    const result = computeWindow({
      sizes: Array.from({ length: 50 }, () => 40),
      scrollOffset: 150,
      viewportSize: 100,
      overscan: 0,
    })
    expect(result.sections.map((s) => s.index)).toEqual([3, 4, 5, 6])
  })

  it('不变量扫描：随机偏移下窗口始终连续、覆盖恰当年区（不跳行不重复）', () => {
    // 混合行高表：每 7 行一个 80px 分组行，其余 36px
    const sizes = Array.from({ length: 997 }, (_, i) => (i % 7 === 3 ? 80 : 36))
    const total = sizes.reduce((a, b) => a + b, 0)
    for (let offset = 0; offset <= total; offset += 97) {
      const result = computeWindow({ sizes, scrollOffset: offset, viewportSize: 333 })
      expectWindowInvariants(result, sizes, offset, 333)
    }
  })
})
