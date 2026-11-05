// 记录读接口（H-48 视图查询 / H-49 单记录）：事实源
// excel_web_api/internal/gateway/records_routes.go 与接口文档 8.0.6。
// H-48 响应 cells_map 值为 protojson common.Value 包装（int64 为字符串），统一由 cell-codec 解码；
// 请求为普通 JSON（游标 query 参数）。
import { extractCursorPage, http, type CursorPage } from '@/core/http'

import { decodeCellsMap } from './cell-codec'

/** 归一后的记录：cellsMap 已解码为普通值，int64 时间戳/版本转 number。 */
export interface RecordRow {
  id: string
  tableId: string
  cellsMap: Record<string, unknown>
  createdBy: string
  createdAt: number
  version: number
}

export interface QueryRecordsParams {
  baseId: string
  tableId: string
  viewId?: string | null
  cursor?: string | null
  limit?: number
}

interface RawRecord {
  id?: string
  table_id?: string
  cells_map?: Record<string, unknown> | null
  created_by?: string
  created_at?: number | string
  version?: number | string
}

interface RawRecordPage {
  record_list?: RawRecord[]
  has_more?: boolean
  next_cursor?: string | null
  unsolved_comment_count_map?: Record<string, number>
}

interface RawRecordResult {
  record?: RawRecord
}

function toNumber(raw: number | string | undefined): number {
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : 0
  if (typeof raw === 'string' && raw.trim() !== '') {
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? parsed : 0
  }
  return 0
}

/** protojson Record → 归一 RecordRow。 */
export function normalizeRecord(raw: RawRecord): RecordRow {
  return {
    id: raw.id ?? '',
    tableId: raw.table_id ?? '',
    cellsMap: decodeCellsMap(raw.cells_map),
    createdBy: raw.created_by ?? '',
    createdAt: toNumber(raw.created_at),
    version: toNumber(raw.version),
  }
}

/** H-48 视图查询：GET /bases/{base_id}/records?table_id=&view_id=&cursor=&limit=。 */
export async function queryRecords(params: QueryRecordsParams): Promise<CursorPage<RecordRow>> {
  const raw = await http.get<RawRecordPage>(
    `/api/v1/bases/${encodeURIComponent(params.baseId)}/records`,
    {
      query: {
        table_id: params.tableId,
        view_id: params.viewId ?? undefined,
        cursor: params.cursor ?? undefined,
        limit: params.limit,
      },
    },
  )
  const page = extractCursorPage<RawRecord>(
    { ...raw, has_more: raw.has_more ?? false, next_cursor: raw.next_cursor ?? null },
    'record_list',
  )
  return {
    items: page.items.map(normalizeRecord),
    hasMore: page.hasMore,
    nextCursor: page.nextCursor ?? null,
  }
}

/** H-49 单记录：GET /records/{record_id} → {record}。 */
export async function fetchRecord(recordId: string): Promise<RecordRow> {
  const raw = await http.get<RawRecordResult>(
    `/api/v1/records/${encodeURIComponent(recordId)}`,
  )
  return normalizeRecord(raw.record ?? {})
}
