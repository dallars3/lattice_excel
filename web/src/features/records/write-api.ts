// 记录写接口（票 06）：H-50 新建 / H-51 单元格更新 / H-52 批量 / H-53 删除 / H-54 撤销删除。
// 事实源 excel_web_api/internal/gateway/records_routes.go（严格 JSON，DisallowUnknownFields）：
// - H-50/H-52/H-53 结构体接受 client_request_id → 允许 httpClient 自动注入；
// - H-51/H-54 不接受 → attachClientRequestId:false（否则网关 1001）。
import { http, isApiError } from '@/core/http'

import { fetchRecord, normalizeRecord, toNumber, type RawRecord, type RecordRow } from './api'
import { decodeCellValue, encodeCellValue } from './cell-codec'

export interface UpdateCellResult {
  readonly value: unknown
  readonly version: number
}

export interface BatchOperationInput {
  readonly recordId: string
  readonly fieldId: string
  readonly value: unknown
  /** 0 = 不校验版本（同一记录多字段批量写入时避免自造冲突）。 */
  readonly expectVersion: number
}

export interface BatchOpResult {
  readonly index: number
  readonly recordId: string
  readonly ok: boolean
  readonly version: number
  readonly errorCode: number
}

export interface BatchUpdateResult {
  readonly resultList: readonly BatchOpResult[]
  readonly failedRows: readonly BatchOpResult[]
  readonly skippedCount: number
}

export interface DeleteRecordsResult {
  readonly deletedCount: number
}

interface RawOpResult {
  index?: number
  record_id?: string
  ok?: boolean
  version?: number | string
  error_code?: number
}

interface RawBatchResult {
  result_list?: RawOpResult[]
  fail_row_list?: RawOpResult[]
  skipped_count?: number
}

interface RawDeleteResult {
  deleted_count?: number
}

interface RawUpdateResult {
  value?: unknown
  version?: number | string
}

interface RawRecordResult {
  record?: RawRecord
}

function normalizeOpResult(raw: RawOpResult): BatchOpResult {
  return {
    index: raw.index ?? 0,
    recordId: raw.record_id ?? '',
    ok: raw.ok ?? false,
    version: toNumber(raw.version),
    errorCode: raw.error_code ?? 0,
  }
}

function encodeCellsMap(cellsMap: Record<string, unknown>): Record<string, unknown> {
  const encoded: Record<string, unknown> = {}
  for (const [fieldId, value] of Object.entries(cellsMap)) {
    encoded[fieldId] = encodeCellValue(value)
  }
  return encoded
}

/** H-50 新建记录：POST /bases/{base_id}/records，body {table_id,cells_map}。 */
export async function createRecord(
  baseId: string,
  tableId: string,
  cellsMap: Record<string, unknown> = {},
): Promise<RecordRow> {
  const raw = await http.post<RawRecordResult>(
    `/api/v1/bases/${encodeURIComponent(baseId)}/records`,
    { table_id: tableId, cells_map: encodeCellsMap(cellsMap) },
  )
  return normalizeRecord(raw.record ?? {})
}

/** H-51 单元格更新：PUT /records/{record_id}/cells/{field_id}?base_id=。 */
export async function updateCell(
  baseId: string,
  recordId: string,
  fieldId: string,
  value: unknown,
  expectVersion: number,
): Promise<UpdateCellResult> {
  const raw = await http.put<RawUpdateResult>(
    `/api/v1/records/${encodeURIComponent(recordId)}/cells/${encodeURIComponent(fieldId)}`,
    { value: encodeCellValue(value), expect_version: expectVersion },
    { query: { base_id: baseId }, attachClientRequestId: false },
  )
  return {
    value: decodeCellValue(raw.value),
    version: toNumber(raw.version),
  }
}

/** H-52 批量更新：POST /records/batch?base_id=，body {op_list}。 */
export async function batchUpdate(
  baseId: string,
  opList: readonly BatchOperationInput[],
): Promise<BatchUpdateResult> {
  const raw = await http.post<RawBatchResult>(
    '/api/v1/records/batch',
    {
      op_list: opList.map((operation) => ({
        record_id: operation.recordId,
        field_id: operation.fieldId,
        value: encodeCellValue(operation.value),
        expect_version: operation.expectVersion,
      })),
    },
    { query: { base_id: baseId } },
  )
  return {
    resultList: (raw.result_list ?? []).map(normalizeOpResult),
    failedRows: (raw.fail_row_list ?? []).map(normalizeOpResult),
    skippedCount: raw.skipped_count ?? 0,
  }
}

/** H-53 删除记录：DELETE /records?base_id=，body {record_id_list}。 */
export async function deleteRecords(
  baseId: string,
  recordIds: readonly string[],
): Promise<DeleteRecordsResult> {
  const raw = await http.delete<RawDeleteResult>(
    '/api/v1/records',
    { record_id_list: [...recordIds] },
    { query: { base_id: baseId } },
  )
  return { deletedCount: raw.deleted_count ?? recordIds.length }
}

/** H-54 撤销删除：POST /records/{record_id}/undo?base_id=（body 空、无 client_request_id）。 */
export async function undoDelete(
  baseId: string,
  recordId: string,
): Promise<RecordRow | null> {
  const raw = await http.post<RawRecordResult>(
    `/api/v1/records/${encodeURIComponent(recordId)}/undo`,
    undefined,
    { query: { base_id: baseId }, attachClientRequestId: false },
  )
  return raw.record === undefined ? null : normalizeRecord(raw.record)
}

/**
 * 复制记录 = H-49 读 + H-50 建（N8 裁定，不新增原子接口）。
 * 公式/引用为只读派生值、空值不写入；评论与历史不复制。
 */
export async function copyRecord(
  baseId: string,
  tableId: string,
  recordId: string,
  fields: readonly { readonly id: string; readonly type: string }[],
): Promise<RecordRow> {
  const source = await fetchRecord(recordId)
  const cellsMap: Record<string, unknown> = {}
  for (const field of fields) {
    if (field.type === 'formula' || field.type === 'lookup') continue
    const value = source.cellsMap[field.id]
    if (value === null || value === undefined) continue
    cellsMap[field.id] = value
  }
  return createRecord(baseId, tableId, cellsMap)
}

export interface ConflictMeta {
  readonly theirValue: unknown
  readonly theirVersion: number
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null
}

/**
 * 4001 冲突元数据：data/meta = {their_value, their_version}。
 * 兼容 ApiError 与结构相同的信封对象；非 4001/缺字段返回 null（调用方按普通失败处理）。
 */
export function parseConflictMeta(error: unknown): ConflictMeta | null {
  let code: number | null = null
  let payload: unknown = null
  if (isApiError(error)) {
    code = error.code
    payload = error.meta
  } else {
    const record = asRecord(error)
    if (record !== null && typeof record.code === 'number') {
      code = record.code
      payload = 'data' in record ? record.data : record.meta
    }
  }
  if (code !== 4001) return null
  const meta = asRecord(payload)
  if (meta === null || !('their_value' in meta) || !('their_version' in meta)) return null
  return {
    theirValue: decodeCellValue(meta.their_value),
    theirVersion: toNumber(
      typeof meta.their_version === 'number' ? meta.their_version : String(meta.their_version),
    ),
  }
}

/** 4002 记录/字段已删除。 */
export function isDeletedError(error: unknown): boolean {
  return isApiError(error) && error.code === 4002
}

/** 5001 容量上限。 */
export function isCapacityError(error: unknown): boolean {
  return isApiError(error) && error.code === 5001
}
