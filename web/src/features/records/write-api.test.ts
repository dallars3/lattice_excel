// 记录写接口（票 06）：H-50/H-51/H-52/H-53/H-54 与 H-49+H-50 复制记录。
// 断言请求形状（严格 JSON 不接受 client_request_id 的三条必须无该字段）与响应归一化。
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'

import { mswServer } from '@/mocks/server'

import type { FieldRecord } from '@/features/workspace/api/field'

import {
  batchUpdate,
  copyRecord,
  createRecord,
  deleteRecords,
  parseConflictMeta,
  updateCell,
  undoDelete,
} from './write-api'

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

const RAW_RECORD = {
  id: 'rec_1',
  table_id: 'tbl_1',
  cells_map: { fld_title: { string_value: '标题一' }, fld_num: { int_value: '0' } },
  created_by: 'usr_1',
  created_at: '1730000000000',
  version: '3',
}

describe('createRecord（H-50）', () => {
  it('body 含 table_id/cells_map，允许自动 client_request_id，响应归一化', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      http.post('*/api/v1/bases/:base_id/records', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return ok({ record: RAW_RECORD })
      }),
    )

    const record = await createRecord('base_1', 'tbl_1', { fld_title: '标题一' })

    expect(body.table_id).toBe('tbl_1')
    expect(body.cells_map).toEqual({ fld_title: '标题一' })
    expect(typeof body.client_request_id).toBe('string')
    expect(record.id).toBe('rec_1')
    expect(record.cellsMap.fld_num).toBe(0)
    expect(record.version).toBe(3)
  })
})

describe('updateCell（H-51）', () => {
  it('PUT cells/{field_id}?base_id=，body 仅 value/expect_version（无 client_request_id）', async () => {
    let body: Record<string, unknown> = {}
    const captured: { url: URL | null } = { url: null }
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        captured.url = new URL(request.url)
        body = (await request.json()) as Record<string, unknown>
        return ok({ value: { string_value: '新值' }, version: '4' })
      }),
    )

    const result = await updateCell('base_1', 'rec_1', 'fld_a', '新值', 3)

    expect(captured.url?.pathname).toBe('/api/v1/records/rec_1/cells/fld_a')
    expect(captured.url?.searchParams.get('base_id')).toBe('base_1')
    expect(body).toEqual({ value: '新值', expect_version: 3 })
    expect(body).not.toHaveProperty('client_request_id')
    expect(result.value).toBe('新值')
    expect(result.version).toBe(4)
  })

  it('value=null 清空：仍显式发送 value:null', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return ok({ version: '5' })
      }),
    )

    await updateCell('base_1', 'rec_1', 'fld_a', null, 4)

    expect(body).toEqual({ value: null, expect_version: 4 })
  })
})

describe('batchUpdate（H-52）', () => {
  it('POST /records/batch?base_id=，op_list 编码普通值并返回 skipped_count/fail_row_list', async () => {
    let body: Record<string, unknown> = {}
    const captured: { url: URL | null } = { url: null }
    mswServer.use(
      http.post('*/api/v1/records/batch', async ({ request }) => {
        captured.url = new URL(request.url)
        body = (await request.json()) as Record<string, unknown>
        return ok({
          result_list: [{ index: 0, record_id: 'rec_1', ok: true, version: '9', error_code: 0 }],
          fail_row_list: [{ index: 1, record_id: 'rec_1', ok: false, error_code: 4001 }],
          skipped_count: 2,
        })
      }),
    )

    const result = await batchUpdate('base_1', [
      { recordId: 'rec_1', fieldId: 'fld_a', value: 'x', expectVersion: 0 },
      { recordId: 'rec_1', fieldId: 'fld_b', value: null, expectVersion: 0 },
    ])

    expect(captured.url?.searchParams.get('base_id')).toBe('base_1')
    expect(body.op_list).toEqual([
      { record_id: 'rec_1', field_id: 'fld_a', value: 'x', expect_version: 0 },
      { record_id: 'rec_1', field_id: 'fld_b', value: null, expect_version: 0 },
    ])
    expect(typeof body.client_request_id).toBe('string')
    expect(result.skippedCount).toBe(2)
    expect(result.failedRows).toHaveLength(1)
  })
})

describe('deleteRecords（H-53）与 undoDelete（H-54）', () => {
  it('DELETE /records?base_id= body record_id_list + client_request_id', async () => {
    let body: Record<string, unknown> = {}
    const captured: { url: URL | null } = { url: null }
    mswServer.use(
      http.delete('*/api/v1/records', async ({ request }) => {
        captured.url = new URL(request.url)
        body = (await request.json()) as Record<string, unknown>
        return ok({ deleted_count: 2 })
      }),
    )

    const result = await deleteRecords('base_1', ['rec_1', 'rec_2'])

    expect(captured.url?.searchParams.get('base_id')).toBe('base_1')
    expect(body.record_id_list).toEqual(['rec_1', 'rec_2'])
    expect(typeof body.client_request_id).toBe('string')
    expect(result.deletedCount).toBe(2)
  })

  it('POST undo?base_id= 无请求体且无 client_request_id', async () => {
    let rawBody = 'unset'
    const captured: { url: URL | null } = { url: null }
    mswServer.use(
      http.post('*/api/v1/records/:record_id/undo', async ({ request }) => {
        captured.url = new URL(request.url)
        rawBody = await request.text()
        return ok({ record: RAW_RECORD })
      }),
    )

    const record = await undoDelete('base_1', 'rec_1')

    expect(captured.url?.searchParams.get('base_id')).toBe('base_1')
    expect(rawBody).toBe('')
    expect(record?.id).toBe('rec_1')
  })
})

describe('copyRecord（H-49 + H-50）', () => {
  const fields: FieldRecord[] = [
    { id: 'fld_title', tableId: 'tbl_1', name: '标题', type: 'text', configMap: {}, isPrimary: true, position: 0, description: '' },
    { id: 'fld_num', tableId: 'tbl_1', name: '金额', type: 'number', configMap: {}, isPrimary: false, position: 1, description: '' },
    { id: 'fld_formula', tableId: 'tbl_1', name: '合计', type: 'formula', configMap: {}, isPrimary: false, position: 2, description: '' },
    { id: 'fld_lookup', tableId: 'tbl_1', name: '引用', type: 'lookup', configMap: {}, isPrimary: false, position: 3, description: '' },
    { id: 'fld_empty', tableId: 'tbl_1', name: '空', type: 'text', configMap: {}, isPrimary: false, position: 4, description: '' },
  ]

  it('读取源记录后仅复制可写且有值的字段，排除公式/引用', async () => {
    let createdBody: Record<string, unknown> = {}
    mswServer.use(
      http.get('*/api/v1/records/:record_id', () => ok({ record: RAW_RECORD })),
      http.post('*/api/v1/bases/:base_id/records', async ({ request }) => {
        createdBody = (await request.json()) as Record<string, unknown>
        return ok({
          record: { ...RAW_RECORD, id: 'rec_copy' },
        })
      }),
    )

    const copy = await copyRecord('base_1', 'tbl_1', 'rec_1', fields)

    expect(copy.id).toBe('rec_copy')
    expect(createdBody.table_id).toBe('tbl_1')
    expect(createdBody.cells_map).toEqual({ fld_title: '标题一', fld_num: 0 })
  })
})

describe('冲突元数据', () => {
  it('4001 的 data {their_value,their_version} 解码为普通值与数字版本', () => {
    const meta = parseConflictMeta({
      code: 4001,
      message: 'x',
      data: { their_value: { string_value: 'B' }, their_version: '8' },
    })
    expect(meta).toEqual({ theirValue: 'B', theirVersion: 8 })
  })

  it('非 4001 / 非法结构返回 null', () => {
    expect(parseConflictMeta(null)).toBeNull()
    expect(parseConflictMeta({ code: 4002, data: null })).toBeNull()
    expect(parseConflictMeta({ code: 4001, data: { their_value: 1 } })).toBeNull()
  })
})
