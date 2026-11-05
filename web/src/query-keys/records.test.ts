// records query key 工厂测试（票 05）：新增表作用域/单视图无限查询/单记录 key，
// 并保持 M1 的 list/page 形状与失效前缀不破坏。
import { describe, expect, it } from 'vitest'

import { recordsKeys } from './records'

describe('recordsKeys（票 05 增补）', () => {
  it('表作用域覆盖该表全部记录查询（写后按表失效）', () => {
    expect(recordsKeys.table('tbl_1')).toEqual(['records', 'table', 'tbl_1'])
    expect(recordsKeys.listFor('tbl_1', 'view_1')).toEqual([
      'records',
      'table',
      'tbl_1',
      'list',
      'view_1',
    ])
    expect(recordsKeys.listFor('tbl_1', 'view_1').slice(0, 3)).toEqual(
      recordsKeys.table('tbl_1'),
    )
  })

  it('单记录 key 独立于视图列表', () => {
    expect(recordsKeys.record('rec_1')).toEqual(['records', 'record', 'rec_1'])
  })

  it('保留 M1 list/page 形状（既有缓存与测试不受影响）', () => {
    expect(recordsKeys.list('view_1')).toEqual(['records', 'view_1'])
    expect(recordsKeys.page('view_1', 'cur_20')).toEqual(['records', 'view_1', 'cur_20'])
  })
})
