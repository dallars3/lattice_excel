// 数据表结构接口（H-29 ~ H-33）：事实源 excel_web_api/internal/gateway/structure_routes.go。
// 网关 decodeJSONBody 使用 DisallowUnknownFields，所有写接口均不接受 client_request_id。
import { http } from '@/core/http'

/** 归一后的数据表摘要（protojson：int64 created_at 为字符串）。 */
export interface TableSummary {
  id: string
  baseId: string
  name: string
  fieldCount: number
  viewCount: number
  createdAt: number
}

interface RawTable {
  id?: string
  base_id?: string
  name?: string
  field_count?: number
  view_count?: number
  created_at?: number | string
}

interface RawTableList {
  table_list?: RawTable[]
}

interface RawTableResult {
  table?: RawTable
}

const STRICT_JSON_WRITE = { attachClientRequestId: false } as const

function normalizeTimestamp(raw: number | string | undefined): number {
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : 0
  if (typeof raw === 'string') {
    const value = Number(raw)
    return Number.isFinite(value) ? value : 0
  }
  return 0
}

function normalizeTable(raw: RawTable): TableSummary {
  return {
    id: raw.id ?? '',
    baseId: raw.base_id ?? '',
    name: raw.name ?? '',
    fieldCount: raw.field_count ?? 0,
    viewCount: raw.view_count ?? 0,
    createdAt: normalizeTimestamp(raw.created_at),
  }
}

/** H-29 表列表：GET /bases/{base_id}/tables → {table_list}。 */
export async function fetchTables(baseId: string): Promise<TableSummary[]> {
  const raw = await http.get<RawTableList>(
    `/api/v1/bases/${encodeURIComponent(baseId)}/tables`,
  )
  return (raw.table_list ?? []).map(normalizeTable)
}

/** H-30 新建表：POST /bases/{base_id}/tables，body 仅 name。 */
export async function createTable(baseId: string, name: string): Promise<TableSummary> {
  const raw = await http.post<RawTableResult>(
    `/api/v1/bases/${encodeURIComponent(baseId)}/tables`,
    { name: name.trim() },
    STRICT_JSON_WRITE,
  )
  return normalizeTable(raw.table ?? {})
}

/** H-31 重命名表：PATCH /tables/{table_id}，body 仅 name。 */
export async function renameTable(tableId: string, name: string): Promise<TableSummary> {
  const raw = await http.patch<RawTableResult>(
    `/api/v1/tables/${encodeURIComponent(tableId)}`,
    { name: name.trim() },
    STRICT_JSON_WRITE,
  )
  return normalizeTable(raw.table ?? {})
}

/** H-32 复制表：POST /tables/{table_id}/duplicate，无请求体（不得注入 client_request_id）。 */
export async function duplicateTable(tableId: string): Promise<TableSummary> {
  const raw = await http.post<RawTableResult>(
    `/api/v1/tables/${encodeURIComponent(tableId)}/duplicate`,
    undefined,
    STRICT_JSON_WRITE,
  )
  return normalizeTable(raw.table ?? {})
}

/** H-33 删除表：DELETE /tables/{table_id}，body 仅 confirm=true。 */
export async function deleteTable(tableId: string): Promise<unknown> {
  return http.delete<unknown>(
    `/api/v1/tables/${encodeURIComponent(tableId)}`,
    { confirm: true },
    STRICT_JSON_WRITE,
  )
}
