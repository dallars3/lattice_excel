// loadedRanges LRU 表驱动测试（票 07，技术方案 6.1「loadedRanges: Map<cursor, Page>」/ 12 章内存「上限 ≈10 页按 LRU 淘汰」）。
// 语义：insert/get 触及刷新新鲜度；超上限淘汰最久未触及页。
import { describe, expect, it } from 'vitest'

import { DEFAULT_LOADED_RANGES_CAPACITY, LoadedRangeCache, type GridPage } from './loaded-ranges'

function page(cursor: string, startIndex: number, endIndex: number): GridPage {
  const count = endIndex - startIndex + 1
  return {
    cursor,
    startIndex,
    endIndex,
    recordIds: Array.from({ length: count }, (_, i) => `r${startIndex + i + 1}`),
  }
}

describe('LoadedRangeCache 基本语义', () => {
  it('默认上限 10 页（技术方案 12 章 ≈10 页）', () => {
    expect(DEFAULT_LOADED_RANGES_CAPACITY).toBe(10)
  })

  it('put 后 get 按 cursor 取回页内容；未命中返回 undefined', () => {
    const cache = new LoadedRangeCache()
    cache.put(page('cur-1', 0, 99))
    const got = cache.get('cur-1')
    expect(got).toBeDefined()
    expect(got?.startIndex).toBe(0)
    expect(got?.endIndex).toBe(99)
    expect(got?.recordIds[0]).toBe('r1')
    expect(got?.recordIds.length).toBe(100)
    expect(cache.get('cur-404')).toBeUndefined()
  })

  it('put 同 cursor 更新页内容（不新增条目）', () => {
    const cache = new LoadedRangeCache({ capacity: 3 })
    cache.put(page('cur-1', 0, 99))
    cache.put(page('cur-1', 0, 49)) // 重置/刷新语义：同 cursor 覆盖
    expect(cache.size).toBe(1)
    expect(cache.get('cur-1')?.endIndex).toBe(49)
  })

  it('clear：筛选/排序/分组变更时清空全部区间（技术方案 6.1 唯一全量重拉场景）', () => {
    const cache = new LoadedRangeCache({ capacity: 2 })
    cache.put(page('a', 0, 9))
    cache.put(page('b', 10, 19))
    cache.clear()
    expect(cache.size).toBe(0)
    expect(cache.get('a')).toBeUndefined()
    expect(cache.get('b')).toBeUndefined()
    // clear 是显式清空，不计入 LRU 淘汰日志
    expect(cache.evicted).toEqual([])
  })

  it('非法上限显式拒绝（capacity <= 0 抛错，不静默吞）', () => {
    expect(() => new LoadedRangeCache({ capacity: 0 })).toThrow()
    expect(() => new LoadedRangeCache({ capacity: -1 })).toThrow()
  })
})

describe('LoadedRangeCache LRU 淘汰', () => {
  // 表驱动：操作序列 → 期望存活集与淘汰序
  // 操作：{op:'put', cursor, start, end} | {op:'get', cursor, hits} | {op:'has', cursor}
  const cases: ReadonlyArray<{
    readonly name: string
    readonly capacity: number
    readonly ops: ReadonlyArray<Record<string, unknown>>
    readonly expectedAlive: readonly string[]
    readonly expectedEvicted: readonly string[]
  }> = [
    {
      name: '超上限插入：淘汰最久未触及（首插且从未触及），容量 3 存活 3 页',
      capacity: 3,
      ops: [
        { op: 'put', cursor: 'a', start: 0, end: 9 },
        { op: 'put', cursor: 'b', start: 10, end: 19 },
        { op: 'put', cursor: 'c', start: 20, end: 29 },
        { op: 'put', cursor: 'd', start: 30, end: 39 },
      ],
      expectedAlive: ['b', 'c', 'd'],
      expectedEvicted: ['a'],
    },
    {
      name: 'get 触及刷新新鲜度：被 get 的页免于淘汰',
      capacity: 3,
      ops: [
        { op: 'put', cursor: 'a', start: 0, end: 9 },
        { op: 'put', cursor: 'b', start: 10, end: 19 },
        { op: 'put', cursor: 'c', start: 20, end: 29 },
        { op: 'get', cursor: 'a', hits: true },
        { op: 'put', cursor: 'd', start: 30, end: 39 },
      ],
      expectedAlive: ['c', 'a', 'd'],
      expectedEvicted: ['b'],
    },
    {
      name: 'put 既有 key 触及刷新：更新页不引发淘汰、免于紧随其后的淘汰',
      capacity: 3,
      ops: [
        { op: 'put', cursor: 'a', start: 0, end: 9 },
        { op: 'put', cursor: 'b', start: 10, end: 19 },
        { op: 'put', cursor: 'c', start: 20, end: 29 },
        { op: 'put', cursor: 'a', start: 0, end: 4 },
        { op: 'put', cursor: 'd', start: 30, end: 39 },
      ],
      expectedAlive: ['c', 'a', 'd'],
      expectedEvicted: ['b'],
    },
    {
      name: 'has 只查询不触及：has 命中不改变新鲜度',
      capacity: 3,
      ops: [
        { op: 'put', cursor: 'a', start: 0, end: 9 },
        { op: 'put', cursor: 'b', start: 10, end: 19 },
        { op: 'put', cursor: 'c', start: 20, end: 29 },
        { op: 'has', cursor: 'c' },
        { op: 'put', cursor: 'd', start: 30, end: 39 },
      ],
      expectedAlive: ['b', 'c', 'd'],
      expectedEvicted: ['a'],
    },
    {
      name: '连续淘汰：容量 1 时每次新插淘汰上一页',
      capacity: 1,
      ops: [
        { op: 'put', cursor: 'a', start: 0, end: 9 },
        { op: 'put', cursor: 'b', start: 10, end: 19 },
        { op: 'put', cursor: 'c', start: 20, end: 29 },
      ],
      expectedAlive: ['c'],
      expectedEvicted: ['a', 'b'],
    },
    {
      name: '未超上限不淘汰',
      capacity: 10,
      ops: [
        { op: 'put', cursor: 'a', start: 0, end: 9 },
        { op: 'put', cursor: 'b', start: 10, end: 19 },
      ],
      expectedAlive: ['a', 'b'],
      expectedEvicted: [],
    },
  ]

  for (const c of cases) {
    it(`${c.name}`, () => {
      const cache = new LoadedRangeCache({ capacity: c.capacity })
      for (const op of c.ops) {
        if (op.op === 'put') {
          cache.put(page(String(op.cursor), Number(op.start), Number(op.end)))
        } else if (op.op === 'get') {
          expect(cache.get(String(op.cursor))).toBeDefined()
        } else if (op.op === 'has') {
          void cache.has(String(op.cursor))
        }
      }
      expect(cache.size).toBe(c.expectedAlive.length)
      expect([...cache.keysByRecency()]).toEqual(c.expectedAlive) // 最旧 → 最新
      expect(cache.evicted.map((p) => p.cursor)).toEqual(c.expectedEvicted)
    })
  }

  it('默认上限 10：第 11 页插入淘汰第 1 页，前 10 页中最早插入者先走', () => {
    const cache = new LoadedRangeCache()
    for (let i = 0; i < 10; i += 1) {
      cache.put(page(`p${i}`, i * 100, i * 100 + 99))
    }
    expect(cache.size).toBe(10)
    cache.put(page('p10', 1000, 1099))
    expect(cache.size).toBe(10)
    expect(cache.has('p0')).toBe(false)
    expect(cache.has('p1')).toBe(true)
    expect(cache.has('p10')).toBe(true)
    expect(cache.evicted.map((p) => p.cursor)).toEqual(['p0'])
  })

  it('淘汰日志携带被淘汰页完整内容（内容不静默丢失的观测通道）', () => {
    const cache = new LoadedRangeCache({ capacity: 1 })
    cache.put(page('victim', 40, 49))
    cache.put(page('next', 50, 59))
    expect(cache.evicted.length).toBe(1)
    expect(cache.evicted[0].cursor).toBe('victim')
    expect(cache.evicted[0].startIndex).toBe(40)
    expect(cache.evicted[0].endIndex).toBe(49)
  })
})
