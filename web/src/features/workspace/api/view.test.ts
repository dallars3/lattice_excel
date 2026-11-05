import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import { fetchViews } from './view'

describe('workspace view api（本票仅 H-41 列表）', () => {
  it('归一 view_list：created_at 字符串转 number，保留 tableId/type', async () => {
    let tableId = ''
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/views',
        onRequest: (request) => {
          tableId = new URL(request.url).pathname.split('/').at(-2) ?? ''
        },
        data: {
          view_list: [
            {
              id: 'view_1',
              table_id: 'tbl_1',
              name: '表格视图',
              type: 'table',
              created_at: '1730000000000',
            },
          ],
        },
      }),
    )

    const views = await fetchViews('tbl_1')

    expect(tableId).toBe('tbl_1')
    expect(views).toEqual([
      {
        id: 'view_1',
        tableId: 'tbl_1',
        name: '表格视图',
        type: 'table',
        createdAt: 1730000000000,
      },
    ])
  })
})
