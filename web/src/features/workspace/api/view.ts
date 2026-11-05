// 视图接口（本票仅 H-41 列表；CRUD 与配置属票 07）。
import { http } from '@/core/http'

/** 归一后的视图摘要（protojson：int64 created_at 为字符串）。 */
export interface ViewSummary {
  id: string
  tableId: string
  name: string
  /** 一期仅 table。 */
  type: string
  createdAt: number
}

interface RawView {
  id?: string
  table_id?: string
  name?: string
  type?: string
  created_at?: number | string
}

interface RawViewList {
  view_list?: RawView[]
}

function normalizeTimestamp(raw: number | string | undefined): number {
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : 0
  if (typeof raw === 'string') {
    const value = Number(raw)
    return Number.isFinite(value) ? value : 0
  }
  return 0
}

/** H-41 视图列表：GET /tables/{table_id}/views → {view_list}。 */
export async function fetchViews(tableId: string): Promise<ViewSummary[]> {
  const raw = await http.get<RawViewList>(
    `/api/v1/tables/${encodeURIComponent(tableId)}/views`,
  )
  return (raw.view_list ?? []).map((view) => ({
    id: view.id ?? '',
    tableId: view.table_id ?? '',
    name: view.name ?? '',
    type: view.type ?? '',
    createdAt: normalizeTimestamp(view.created_at),
  }))
}
