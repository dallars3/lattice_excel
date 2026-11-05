import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import {
  createTable,
  deleteTable,
  duplicateTable,
  fetchTables,
  renameTable,
} from './table'

/** protojson 形状：int64 created_at 为字符串，表计数字段为 number。 */
const RAW_TABLE = {
  id: 'tbl_1',
  base_id: 'base_1',
  name: '项目排期',
  field_count: 3,
  view_count: 2,
  created_at: '1730000000000',
}

describe('workspace table api（H-29 ~ H-33）', () => {
  it('H-29 列表：table_list 归一为 camelCase，created_at 字符串转 number', async () => {
    let baseId = ''
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/tables',
        onRequest: (request) => {
          baseId = new URL(request.url).pathname.split('/').at(-2) ?? ''
        },
        data: { table_list: [RAW_TABLE] },
      }),
    )

    const tables = await fetchTables('base_1')

    expect(baseId).toBe('base_1')
    expect(tables).toEqual([
      {
        id: 'tbl_1',
        baseId: 'base_1',
        name: '项目排期',
        fieldCount: 3,
        viewCount: 2,
        createdAt: 1730000000000,
      },
    ])
  })

  it('H-30 新建：body 仅 name，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/tables',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { table: { ...RAW_TABLE, id: 'tbl_new', name: '客户档案' } },
      }),
    )

    const table = await createTable('base_1', '客户档案')

    expect(body).toEqual({ name: '客户档案' })
    expect(table).toMatchObject({ id: 'tbl_new', name: '客户档案', createdAt: 1730000000000 })
  })

  it('H-31 重命名：PATCH body 仅 name，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id',
        method: 'patch',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { table: { ...RAW_TABLE, name: '新表名' } },
      }),
    )

    const table = await renameTable('tbl_1', '新表名')

    expect(body).toEqual({ name: '新表名' })
    expect(table.name).toBe('新表名')
  })

  it('H-32 复制：POST 无请求体（不得注入 client_request_id）', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/duplicate',
        method: 'post',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: { table: { ...RAW_TABLE, id: 'tbl_copy', name: '项目排期 副本' } },
      }),
    )

    const table = await duplicateTable('tbl_1')

    expect(rawBody).toBe('')
    expect(table.id).toBe('tbl_copy')
  })

  it('H-33 删除：DELETE body 仅 confirm=true，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id',
        method: 'delete',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: {},
      }),
    )

    await deleteTable('tbl_1')

    expect(body).toEqual({ confirm: true })
  })
})
