import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import {
  createView,
  deleteView,
  duplicateView,
  fetchViewConfig,
  fetchViews,
  renameView,
  updateViewConfig,
} from './view'

const RAW_VIEW = {
  id: 'view_1',
  table_id: 'tbl_1',
  name: '表格视图',
  type: 'table',
  created_at: '1730000000000',
}

const NORMALIZED_VIEW = {
  id: 'view_1',
  tableId: 'tbl_1',
  name: '表格视图',
  type: 'table',
  createdAt: 1730000000000,
}

describe('workspace view api（H-41 列表）', () => {
  it('归一 view_list：created_at 字符串转 number，保留 tableId/type', async () => {
    let tableId = ''
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/views',
        onRequest: (request) => {
          tableId = new URL(request.url).pathname.split('/').at(-2) ?? ''
        },
        data: { view_list: [RAW_VIEW] },
      }),
    )

    const views = await fetchViews('tbl_1')

    expect(tableId).toBe('tbl_1')
    expect(views).toEqual([NORMALIZED_VIEW])
  })
})

describe('视图 CRUD（H-42 ~ H-45）', () => {
  it('H-42 新建：POST body 仅 name/type=table，不注入 client_request_id，返回新视图', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/views',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { view: RAW_VIEW },
      }),
    )

    const created = await createView('tbl_1', '表格视图')

    expect(body).toEqual({ name: '表格视图', type: 'table' })
    expect(body).not.toHaveProperty('client_request_id')
    expect(created).toEqual(NORMALIZED_VIEW)
  })

  it('H-43 重命名：PATCH body 仅 name，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/views/:view_id',
        method: 'patch',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { view: { ...RAW_VIEW, name: '新视图名' } },
      }),
    )

    const renamed = await renameView('view_1', '新视图名')

    expect(body).toEqual({ name: '新视图名' })
    expect(body).not.toHaveProperty('client_request_id')
    expect(renamed.name).toBe('新视图名')
  })

  it('H-44 复制：POST 无请求体，不注入 client_request_id', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/views/:view_id/duplicate',
        method: 'post',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: { view: { ...RAW_VIEW, id: 'view_copy', name: '表格视图 副本' } },
      }),
    )

    const copy = await duplicateView('view_1')

    expect(rawBody).toBe('')
    expect(copy.id).toBe('view_copy')
  })

  it('H-45 删除：DELETE 无请求体，不注入 client_request_id', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/views/:view_id',
        method: 'delete',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: {},
      }),
    )

    await deleteView('view_1')

    expect(rawBody).toBe('')
  })
})

describe('视图配置读写（H-46 / H-47）', () => {
  it('H-46 读取 protojson 并解码为内部模型', async () => {
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/views/:view_id/config',
        data: {
          view_config: {
            hidden_field_list: ['fld_2'],
            frozen_field_list: ['fld_1'],
            filter_list: [
              {
                field_id: 'fld_1',
                op: 'VIEW_FILTER_OP_IN',
                value: {
                  list_value: {
                    value_list: [{ string_value: 'opt_1' }, { string_value: 'opt_2' }],
                  },
                },
              },
              { field_id: 'fld_2', op: 'VIEW_FILTER_OP_IS_EMPTY' },
            ],
            sort_list: [{ field_id: 'fld_1', dir: 'SORT_DIR_DESC' }],
            group_by_list: [{ field_id: 'fld_date', granularity: 'GRANULARITY_MONTH' }],
            row_height: 'ROW_HEIGHT_COMPACT',
            field_width_map: { fld_1: 200, fld_2: 5000 },
          },
        },
      }),
    )

    const config = await fetchViewConfig('view_1')

    expect(config).toEqual({
      hiddenFieldIds: ['fld_2'],
      frozenFieldIds: ['fld_1'],
      filters: [
        { fieldId: 'fld_1', op: 'in', value: ['opt_1', 'opt_2'] },
        { fieldId: 'fld_2', op: 'is_empty', value: null },
      ],
      sorts: [{ fieldId: 'fld_1', dir: 'desc' }],
      group: { fieldId: 'fld_date', granularity: 'month' },
      rowHeight: 'compact',
      fieldWidthMap: { fld_1: 200, fld_2: 2000 },
    })
  })

  it('H-47 全量写回：body 为 {view_config}，枚举名与 common.Value 包装正确，成功回读', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/views/:view_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: {
          view_config: {
            hidden_field_list: ['fld_2'],
            frozen_field_list: [],
            filter_list: [
              { field_id: 'fld_1', op: 'VIEW_FILTER_OP_BETWEEN', value: { list_value: { value_list: [{ int_value: '1' }, { int_value: '5' }] } } },
            ],
            sort_list: [{ field_id: 'fld_1', dir: 'SORT_DIR_ASC' }],
            group_by_list: [],
            row_height: 'ROW_HEIGHT_TALL',
            field_width_map: { fld_1: 240 },
          },
        },
      }),
    )

    const saved = await updateViewConfig('view_1', {
      hiddenFieldIds: ['fld_2'],
      frozenFieldIds: [],
      filters: [{ fieldId: 'fld_1', op: 'between', value: [1, 5] }],
      sorts: [{ fieldId: 'fld_1', dir: 'asc' }],
      group: null,
      rowHeight: 'tall',
      fieldWidthMap: { fld_1: 240 },
    })

    expect(body).toEqual({
      view_config: {
        hidden_field_list: ['fld_2'],
        frozen_field_list: [],
        filter_list: [
          {
            field_id: 'fld_1',
            op: 'VIEW_FILTER_OP_BETWEEN',
            value: { list_value: { value_list: [{ int_value: '1' }, { int_value: '5' }] } },
          },
        ],
        sort_list: [{ field_id: 'fld_1', dir: 'SORT_DIR_ASC' }],
        group_by_list: [],
        row_height: 'ROW_HEIGHT_TALL',
        field_width_map: { fld_1: 240 },
      },
    })
    expect(body).not.toHaveProperty('client_request_id')
    expect(saved.rowHeight).toBe('tall')
    expect(saved.fieldWidthMap).toEqual({ fld_1: 240 })
  })
})
