import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import {
  convertField,
  createField,
  deleteField,
  duplicateField,
  fetchFields,
  renameField,
  updateFieldConfig,
  updateFieldOptions,
} from './field'

/** protojson 形状：type 为枚举名，config_map 为普通对象，position/is_primary 为原生类型。 */
const RAW_TEXT = {
  id: 'fld_1',
  table_id: 'tbl_1',
  name: '标题',
  type: 'FIELD_TYPE_TEXT',
  config_map: {},
  is_primary: true,
  position: 0,
  description: '记录标题',
}

const RAW_NUMBER = {
  id: 'fld_2',
  table_id: 'tbl_1',
  name: '金额',
  type: 'FIELD_TYPE_NUMBER',
  config_map: { format: 'decimal', decimals: 2, currency: '$', thousands: true },
  is_primary: false,
  position: 1,
  description: '',
}

describe('workspace field api（H-34 ~ H-40、H-92）', () => {
  it('H-34 列表：枚举名转小写类型、config_map 保留对象、description 保留、按 position 归一', async () => {
    let tableId = ''
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/fields',
        onRequest: (request) => {
          tableId = new URL(request.url).pathname.split('/').at(-2) ?? ''
        },
        data: { field_list: [RAW_TEXT, RAW_NUMBER] },
      }),
    )

    const fields = await fetchFields('tbl_1')

    expect(tableId).toBe('tbl_1')
    expect(fields).toEqual([
      {
        id: 'fld_1',
        tableId: 'tbl_1',
        name: '标题',
        type: 'text',
        configMap: {},
        isPrimary: true,
        position: 0,
        description: '记录标题',
      },
      {
        id: 'fld_2',
        tableId: 'tbl_1',
        name: '金额',
        type: 'number',
        configMap: { format: 'decimal', decimals: 2, currency: '$', thousands: true },
        isPrimary: false,
        position: 1,
        description: '',
      },
    ])
  })

  it('H-35 新建：body 仅 name/type/config_map，type 小写且不发送 description 与 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/fields',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { field: { ...RAW_NUMBER, id: 'fld_new' } },
      }),
    )

    const field = await createField('tbl_1', {
      name: '新字段',
      type: 'number',
      configMap: { format: 'int', decimals: 0, currency: '¥', thousands: false },
    })

    expect(body).toEqual({
      name: '新字段',
      type: 'number',
      config_map: { format: 'int', decimals: 0, currency: '¥', thousands: false },
    })
    expect(body).not.toHaveProperty('description')
    expect(body).not.toHaveProperty('client_request_id')
    expect(field).toMatchObject({ id: 'fld_new', type: 'number' })
  })

  it('H-36 重命名：PATCH body 仅 name，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id',
        method: 'patch',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { field: { ...RAW_TEXT, name: '新标题' } },
      }),
    )

    const field = await renameField('fld_1', ' 新标题 ')

    expect(body).toEqual({ name: '新标题' })
    expect(field.name).toBe('新标题')
  })

  it('H-37 更新配置：PUT body 仅 config_map，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { field: { ...RAW_TEXT, config_map: { format: 'percent' } } },
      }),
    )

    await updateFieldConfig('fld_1', { format: 'percent' })

    expect(body).toEqual({ config_map: { format: 'percent' } })
  })

  it('H-38 转换预览：confirm=false 返回受影响数与清空数；确认提交 confirm=true 返回字段', async () => {
    const bodies: unknown[] = []
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/convert',
        method: 'post',
        onRequest: async (request) => {
          bodies.push(await request.json())
        },
        data: { affected_count: 3, cleared_count: 1 },
      }),
    )

    const preview = await convertField('fld_1', 'number', false)
    expect(preview).toMatchObject({ affectedCount: 3, clearedCount: 1, field: null })

    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/convert',
        method: 'post',
        onRequest: async (request) => {
          bodies.push(await request.json())
        },
        data: { field: { ...RAW_NUMBER, id: 'fld_1' }, affected_count: 3, cleared_count: 1 },
      }),
    )
    const converted = await convertField('fld_1', 'number', true)
    expect(converted.field).toMatchObject({ id: 'fld_1', type: 'number' })

    expect(bodies).toEqual([
      { target_type: 'number', confirm: false },
      { target_type: 'number', confirm: true },
    ])
  })

  it('H-39 删除：DELETE body 仅 confirm=true，并解码影响面出参', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id',
        method: 'delete',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { record_count: 12, dependent_formulas: 2, dependent_lookups: 1 },
      }),
    )

    const impact = await deleteField('fld_2')

    expect(body).toEqual({ confirm: true })
    expect(impact).toEqual({ recordCount: 12, dependentFormulas: 2, dependentLookups: 1 })
  })

  it('H-40 选项维护：option_list + allow_create，新选项不带 id，不注入 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/options',
        method: 'put',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { field: { ...RAW_TEXT, type: 'FIELD_TYPE_SELECT' } },
      }),
    )

    await updateFieldOptions(
      'fld_1',
      [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { name: '新选项', color: 'green' },
      ],
      true,
    )

    expect(body).toEqual({
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { name: '新选项', color: 'green' },
      ],
      allow_create: true,
    })
  })

  it('H-92 复制字段：POST 无请求体（不得注入 client_request_id）', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/duplicate',
        method: 'post',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: { field: { ...RAW_NUMBER, id: 'fld_copy', name: '金额 的副本' } },
      }),
    )

    const copy = await duplicateField('fld_2')

    expect(rawBody).toBe('')
    expect(copy).toMatchObject({ id: 'fld_copy', name: '金额 的副本', type: 'number' })
  })
})
