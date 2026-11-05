import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import {
  fetchViewConfigRaw,
  removeFieldFromViewConfig,
  removeFieldFromViews,
  updateViewConfigRaw,
} from './view-config-raw'

const RAW_CONFIG = {
  hidden_field_list: ['fld_2', 'fld_3'],
  frozen_field_list: ['fld_1', 'fld_2'],
  filter_list: [
    { field_id: 'fld_2', op: 'VIEW_FILTER_OP_CONTAINS', value: { string_value: 'x' } },
    { field_id: 'fld_3', op: 'VIEW_FILTER_OP_IS_EMPTY' },
  ],
  sort_list: [
    { field_id: 'fld_2', dir: 'SORT_DIR_ASC' },
    { field_id: 'fld_3', dir: 'SORT_DIR_DESC' },
  ],
  group_by_list: [{ field_id: 'fld_2', granularity: 'GRANULARITY_MONTH' }],
  row_height: 'ROW_HEIGHT_MEDIUM',
  field_width_map: { fld_1: 200, fld_2: 320 },
}

describe('视图配置原始形状（H-46 / H-47）', () => {
  it('H-46 读取原始 view_config（protojson 形状不简化）', async () => {
    let viewId = ''
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/views/:view_id/config',
        onRequest: (request) => {
          viewId = new URL(request.url).pathname.split('/').at(-2) ?? ''
        },
        data: { view_config: RAW_CONFIG },
      }),
    )

    const config = await fetchViewConfigRaw('view_1')

    expect(viewId).toBe('view_1')
    expect(config).toEqual(RAW_CONFIG)
  })

  it('H-47 全量写回：body 为 {view_config: 原始对象}，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/views/:view_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { view_config: RAW_CONFIG },
      }),
    )

    const saved = await updateViewConfigRaw('view_1', RAW_CONFIG)

    expect(body).toEqual({ view_config: RAW_CONFIG })
    expect(body).not.toHaveProperty('client_request_id')
    expect(saved).toEqual(RAW_CONFIG)
  })
})

describe('删除字段后的视图条件清理（纯函数）', () => {
  it('移除隐藏/冻结/筛选/排序/分组/字段宽度中对目标字段的引用，保留其他字段', () => {
    const result = removeFieldFromViewConfig(RAW_CONFIG, 'fld_2')

    expect(result.changed).toBe(true)
    expect(result.config).toEqual({
      hidden_field_list: ['fld_3'],
      frozen_field_list: ['fld_1'],
      filter_list: [{ field_id: 'fld_3', op: 'VIEW_FILTER_OP_IS_EMPTY' }],
      sort_list: [{ field_id: 'fld_3', dir: 'SORT_DIR_DESC' }],
      group_by_list: [],
      row_height: 'ROW_HEIGHT_MEDIUM',
      field_width_map: { fld_1: 200 },
    })
  })

  it('未引用目标字段时标记 changed=false 且不改动配置', () => {
    const result = removeFieldFromViewConfig(RAW_CONFIG, 'fld_missing')

    expect(result.changed).toBe(false)
    expect(result.config).toEqual(RAW_CONFIG)
  })

  it('批量清理返回受影响视图列表', () => {
    const result = removeFieldFromViews(
      [
        { viewId: 'view_1', config: RAW_CONFIG },
        { viewId: 'view_2', config: { hidden_field_list: ['fld_3'] } },
      ],
      'fld_2',
    )

    expect(result.affectedViewIds).toEqual(['view_1'])
    expect(result.cleaned[0]).toMatchObject({ viewId: 'view_1', changed: true })
    expect(result.cleaned[1]).toMatchObject({ viewId: 'view_2', changed: false })
  })
})
