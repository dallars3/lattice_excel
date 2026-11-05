// 视图接口（H-41 ~ H-47）：事实源 excel_web_api/internal/gateway/structure_routes.go。
// 网关 decodeJSONBody 使用 DisallowUnknownFields，视图写接口均不接受 client_request_id；
// H-46/H-47 的 protojson 形状由 view-config-codec 归一，避免 UI 接触枚举名。
import { http } from '@/core/http'

import { decodeViewConfig, encodeViewConfig } from '../types/view-config-codec'
import type { ViewConfig } from '../types/view-config'
import { fetchViewConfigRaw, updateViewConfigRaw } from './view-config-raw'

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

interface RawViewResult {
  view?: RawView
}

function normalizeTimestamp(raw: number | string | undefined): number {
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : 0
  if (typeof raw === 'string') {
    const value = Number(raw)
    return Number.isFinite(value) ? value : 0
  }
  return 0
}

function normalizeView(raw: RawView): ViewSummary {
  return {
    id: raw.id ?? '',
    tableId: raw.table_id ?? '',
    name: raw.name ?? '',
    type: raw.type ?? '',
    createdAt: normalizeTimestamp(raw.created_at),
  }
}

const STRICT_JSON_WRITE = { attachClientRequestId: false } as const

/** H-41 视图列表：GET /tables/{table_id}/views → {view_list}。 */
export async function fetchViews(tableId: string): Promise<ViewSummary[]> {
  const raw = await http.get<RawViewList>(
    `/api/v1/tables/${encodeURIComponent(tableId)}/views`,
  )
  return (raw.view_list ?? []).map(normalizeView)
}

/** H-42 新建视图：POST /tables/{table_id}/views，body {name, type:"table"}。 */
export async function createView(tableId: string, name: string): Promise<ViewSummary> {
  const raw = await http.post<RawViewResult>(
    `/api/v1/tables/${encodeURIComponent(tableId)}/views`,
    { name: name.trim(), type: 'table' },
    STRICT_JSON_WRITE,
  )
  return normalizeView(raw.view ?? {})
}

/** H-43 重命名视图：PATCH /views/{view_id}，body 仅 name。 */
export async function renameView(viewId: string, name: string): Promise<ViewSummary> {
  const raw = await http.patch<RawViewResult>(
    `/api/v1/views/${encodeURIComponent(viewId)}`,
    { name: name.trim() },
    STRICT_JSON_WRITE,
  )
  return normalizeView(raw.view ?? {})
}

/** H-44 复制视图：POST /views/{view_id}/duplicate，无请求体。 */
export async function duplicateView(viewId: string): Promise<ViewSummary> {
  const raw = await http.post<RawViewResult>(
    `/api/v1/views/${encodeURIComponent(viewId)}/duplicate`,
    undefined,
    STRICT_JSON_WRITE,
  )
  return normalizeView(raw.view ?? {})
}

/** H-45 删除视图：DELETE /views/{view_id}，无请求体（至少保留 1 个视图由入口禁用兜底）。 */
export async function deleteView(viewId: string): Promise<void> {
  await http.delete<Record<string, never>>(
    `/api/v1/views/${encodeURIComponent(viewId)}`,
    undefined,
    STRICT_JSON_WRITE,
  )
}

/** H-46 读取视图配置：protojson → 内部模型。 */
export async function fetchViewConfig(viewId: string): Promise<ViewConfig> {
  return decodeViewConfig(await fetchViewConfigRaw(viewId))
}

/** H-47 全量替换视图配置：内部模型 → {view_config}，成功回读服务端响应。 */
export async function updateViewConfig(
  viewId: string,
  config: ViewConfig,
): Promise<ViewConfig> {
  const saved = await updateViewConfigRaw(viewId, encodeViewConfig(config))
  return decodeViewConfig(saved)
}
