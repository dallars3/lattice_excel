// records API 测试（票 05）：H-48 游标查询与 H-49 单记录读取，断言请求参数与 protojson 归一化。
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'

import { mswServer } from '@/mocks/server'

import { fetchRecord, queryRecords } from './api'

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

const RAW_RECORD = {
  id: 'rec_1',
  table_id: 'tbl_1',
  cells_map: {
    fld_text: { string_value: '标题一' },
    fld_number: { int_value: '0' },
    fld_date: { date_value_ms: '1730000000000' },
    fld_multi: { list_value: { value_list: [{ string_value: 'opt_1' }, { string_value: 'opt_3' }] } },
    fld_empty: null,
  },
  created_by: 'usr_1',
  created_at: '1730000000000',
  version: '3',
}

describe('queryRecords（H-48）', () => {
  it('携带 table_id/view_id/cursor/limit 并归一化 record_list 与游标字段', async () => {
    const captured: { url: URL | null } = { url: null }
    mswServer.use(
      http.get('*/api/v1/bases/:base_id/records', ({ request }) => {
        captured.url = new URL(request.url)
        return ok({
          record_list: [RAW_RECORD],
          has_more: true,
          next_cursor: 'cur_20',
          unsolved_comment_count_map: { rec_1: 2 },
        })
      }),
    )

    const page = await queryRecords({
      baseId: 'base_1',
      tableId: 'tbl_1',
      viewId: 'view_1',
      cursor: 'cur_10',
      limit: 20,
    })

    expect(captured.url?.pathname).toBe('/api/v1/bases/base_1/records')
    expect(captured.url?.searchParams.get('table_id')).toBe('tbl_1')
    expect(captured.url?.searchParams.get('view_id')).toBe('view_1')
    expect(captured.url?.searchParams.get('cursor')).toBe('cur_10')
    expect(captured.url?.searchParams.get('limit')).toBe('20')
    expect(page.hasMore).toBe(true)
    expect(page.nextCursor).toBe('cur_20')
    expect(page.items).toEqual([
      {
        id: 'rec_1',
        tableId: 'tbl_1',
        cellsMap: {
          fld_text: '标题一',
          fld_number: 0,
          fld_date: 1730000000000,
          fld_multi: ['opt_1', 'opt_3'],
          fld_empty: null,
        },
        createdBy: 'usr_1',
        createdAt: 1730000000000,
        version: 3,
      },
    ])
  })

  it('首页 cursor 省略 view_id 时只发 table_id/limit，空记录返回 has_more=false', async () => {
    const captured: { url: URL | null } = { url: null }
    mswServer.use(
      http.get('*/api/v1/bases/:base_id/records', ({ request }) => {
        captured.url = new URL(request.url)
        return ok({ record_list: [], has_more: false, next_cursor: null })
      }),
    )

    const page = await queryRecords({ baseId: 'base_1', tableId: 'tbl_1', limit: 20 })

    expect(captured.url?.searchParams.get('view_id')).toBeNull()
    expect(captured.url?.searchParams.get('cursor')).toBeNull()
    expect(page.items).toEqual([])
    expect(page.hasMore).toBe(false)
  })

  it('列表字段缺失时暴露契约违背', async () => {
    mswServer.use(http.get('*/api/v1/bases/:base_id/records', () => ok({ has_more: false })))

    await expect(queryRecords({ baseId: 'base_1', tableId: 'tbl_1' })).rejects.toThrow(
      '游标分页契约违背',
    )
  })
})

describe('fetchRecord（H-49）', () => {
  it('按 record_id 请求并归一化单条记录', async () => {
    mswServer.use(
      http.get('*/api/v1/records/:record_id', () => ok({ record: RAW_RECORD })),
    )

    const record = await fetchRecord('rec_1')

    expect(record.id).toBe('rec_1')
    expect(record.cellsMap.fld_text).toBe('标题一')
    expect(record.version).toBe(3)
  })

  it('记录不存在时抛 4002 ApiError', async () => {
    mswServer.use(
      http.get('*/api/v1/records/:record_id', () =>
        HttpResponse.json(
          { code: 4002, message: '记录不存在', data: null },
          { status: 404 },
        ),
      ),
    )

    await expect(fetchRecord('rec_deleted')).rejects.toMatchObject({ code: 4002 })
  })
})
