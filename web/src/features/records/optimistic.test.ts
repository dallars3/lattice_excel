// 乐观更新纯逻辑（票 06）：给定 InfiniteQuery 数据结构与单元格补丁，产出不可变新结构。
// 覆盖：成功落版本 / 失败回滚 / 0 与 null 严格保留。
import { describe, expect, it } from 'vitest'

import type { RecordRow } from './api'
import {
  appendRecord,
  applyOptimisticCell,
  findRecord,
  removeRecords,
  rollbackCell,
  type RecordsData,
} from './optimistic'

function record(id: string, cellsMap: Record<string, unknown>, version = 1): RecordRow {
  return {
    id,
    tableId: 'tbl_1',
    cellsMap,
    createdBy: 'usr_1',
    createdAt: 0,
    version,
  }
}

function data(rows: RecordRow[]): RecordsData {
  return {
    pages: [{ items: rows, hasMore: false, nextCursor: null }],
    pageParams: [null],
  }
}

describe('applyOptimisticCell', () => {
  it('命中记录：写入新值并落版本，原数据结构不被修改', () => {
    const before = data([record('rec_1', { fld_a: 'old' }, 1), record('rec_2', { fld_a: 'x' }, 3)])
    const after = applyOptimisticCell(before, {
      recordId: 'rec_1',
      fieldId: 'fld_a',
      value: 'new',
      version: 2,
    })

    expect(after).not.toBe(before)
    expect(after?.pages[0]).not.toBe(before.pages[0])
    expect(findRecord(after, 'rec_1')?.cellsMap.fld_a).toBe('new')
    expect(findRecord(after, 'rec_1')?.version).toBe(2)
    // 其他记录与原始数据保持不变
    expect(findRecord(after, 'rec_2')?.cellsMap.fld_a).toBe('x')
    expect(before.pages[0]?.items[0]?.cellsMap.fld_a).toBe('old')
    expect(before.pages[0]?.items[0]?.version).toBe(1)
  })

  it('未提供 version 时保留原版本（提交期间只改值）', () => {
    const after = applyOptimisticCell(data([record('rec_1', {}, 7)]), {
      recordId: 'rec_1',
      fieldId: 'fld_a',
      value: 'new',
    })
    expect(findRecord(after, 'rec_1')?.version).toBe(7)
  })

  it('0 与 null 严格保留，不被当作空值丢弃', () => {
    const after = applyOptimisticCell(data([record('rec_1', { fld_a: 5 })]), {
      recordId: 'rec_1',
      fieldId: 'fld_a',
      value: 0,
    })
    expect(findRecord(after, 'rec_1')?.cellsMap.fld_a).toBe(0)

    const cleared = applyOptimisticCell(after, {
      recordId: 'rec_1',
      fieldId: 'fld_a',
      value: null,
    })
    expect(findRecord(cleared, 'rec_1')?.cellsMap.fld_a).toBeNull()
    expect(Object.prototype.hasOwnProperty.call(findRecord(cleared, 'rec_1')?.cellsMap, 'fld_a')).toBe(
      true,
    )
  })

  it('记录不存在时原样返回（不虚构行）', () => {
    const before = data([record('rec_1', {})])
    expect(
      applyOptimisticCell(before, { recordId: 'rec_missing', fieldId: 'fld_a', value: 1 }),
    ).toBe(before)
  })

  it('无缓存时返回 undefined（调用方跳过写入）', () => {
    expect(
      applyOptimisticCell(undefined, { recordId: 'rec_1', fieldId: 'fld_a', value: 1 }),
    ).toBeUndefined()
  })
})

describe('rollbackCell', () => {
  it('失败回滚：恢复旧值与旧版本', () => {
    const committed = applyOptimisticCell(data([record('rec_1', { fld_a: 'old' }, 1)]), {
      recordId: 'rec_1',
      fieldId: 'fld_a',
      value: 'new',
    })
    const rolled = rollbackCell(committed, {
      recordId: 'rec_1',
      fieldId: 'fld_a',
      value: 'old',
      version: 1,
    })
    expect(findRecord(rolled, 'rec_1')?.cellsMap.fld_a).toBe('old')
    expect(findRecord(rolled, 'rec_1')?.version).toBe(1)
  })
})

describe('removeRecords / appendRecord', () => {
  it('删除记录跨页过滤，其余页与行保持引用不变', () => {
    const before: RecordsData = {
      pages: [
        { items: [record('rec_1', {}), record('rec_2', {})], hasMore: true, nextCursor: 'c1' },
        { items: [record('rec_3', {})], hasMore: false, nextCursor: null },
      ],
      pageParams: [null, 'c1'],
    }
    const after = removeRecords(before, ['rec_1', 'rec_3'])
    expect(after!.pages[0]?.items.map((row) => row.id)).toEqual(['rec_2'])
    expect(after!.pages[1]?.items).toEqual([])
    expect(after!.pages[0]?.hasMore).toBe(true)
    expect(before.pages[0]?.items).toHaveLength(2)
  })

  it('追加记录到末页；无页时建立首页', () => {
    const empty: RecordsData = { pages: [], pageParams: [] }
    const appended = appendRecord(empty, record('rec_new', { fld_a: 'x' }))
    expect(appended!.pages).toHaveLength(1)
    expect(appended!.pages[0]?.items[0]?.id).toBe('rec_new')

    const again = appendRecord(appended, record('rec_2', {}))
    expect(again!.pages[0]?.items.map((row) => row.id)).toEqual(['rec_new', 'rec_2'])
    expect(empty.pages).toHaveLength(0)
  })
})
