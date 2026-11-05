// 首页与 Base 资产接口（H-21 ~ H-27）：事实源 excel_web_api/internal/gateway/workspace_routes.go。
// 后端 decodeJSONBody 使用 DisallowUnknownFields，写接口均不接收 client_request_id。
import { http } from '@/core/http'

/** 新建未命名 Base 的默认名称（需求 §3.1 / 票 02）。 */
export const DEFAULT_BASE_NAME = '未命名多维表格'

/** 归一后的 Base 摘要：H-21 与 protojson（H-22/23/24/27）形状差异在此收敛。 */
export interface BaseSummary {
  id: string
  name: string
  tableCount: number
  /** 原始角色（owner/manager/editor/filler/viewer），由权限门归一。 */
  myRole: string
  starred: boolean
  /** 毫秒时间戳：H-21 为 number，protojson 为 int64 字符串。 */
  lastModifiedAt: number
  lastModifier: string
}

export interface HomeAggregate {
  recentList: BaseSummary[]
  createdList: BaseSummary[]
  sharedList: BaseSummary[]
  starList: BaseSummary[]
}

interface RawBase {
  id?: string
  name?: string
  table_count?: number
  my_role?: string
  starred?: boolean
  last_modified_at?: number | string
  last_modifier?: string
}

interface RawHome {
  recent_list?: RawBase[]
  created_list?: RawBase[]
  shared_list?: RawBase[]
  star_list?: RawBase[]
}

interface RawBaseList {
  base_list?: RawBase[]
}

interface RawBaseResult {
  base?: RawBase
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

function normalizeBase(raw: RawBase): BaseSummary {
  return {
    id: raw.id ?? '',
    name: raw.name ?? '',
    tableCount: raw.table_count ?? 0,
    myRole: raw.my_role ?? '',
    starred: raw.starred ?? false,
    lastModifiedAt: normalizeTimestamp(raw.last_modified_at),
    lastModifier: raw.last_modifier ?? '',
  }
}

function normalizeBaseList(list: RawBase[] | undefined): BaseSummary[] {
  return (list ?? []).map(normalizeBase)
}

/** H-21 首页聚合：网关直接返回四列表（JSON 数字时间戳）。 */
export async function fetchHome(): Promise<HomeAggregate> {
  const raw = await http.get<RawHome>('/api/v1/home')
  return {
    recentList: normalizeBaseList(raw.recent_list),
    createdList: normalizeBaseList(raw.created_list),
    sharedList: normalizeBaseList(raw.shared_list),
    starList: normalizeBaseList(raw.star_list),
  }
}

/** H-22 名称搜索：q 由调用方保证 1–50 字符（网关空 q 返回 1001）。 */
export async function searchBases(q: string): Promise<BaseSummary[]> {
  const raw = await http.get<RawBaseList>('/api/v1/bases', { query: { q } })
  return normalizeBaseList(raw.base_list)
}

/** H-23 新建 Base：空名落默认名称；成功 data = {base}。 */
export async function createBase(name = ''): Promise<BaseSummary> {
  const trimmed = name.trim()
  const payload = { name: trimmed === '' ? DEFAULT_BASE_NAME : trimmed }
  const raw = await http.post<RawBaseResult>('/api/v1/bases', payload, STRICT_JSON_WRITE)
  return normalizeBase(raw.base ?? {})
}

/** H-24 重命名：PATCH /bases/{id}，body 仅 name。 */
export async function renameBase(baseId: string, name: string): Promise<BaseSummary> {
  const raw = await http.patch<RawBaseResult>(
    `/api/v1/bases/${encodeURIComponent(baseId)}`,
    { name: name.trim() },
    STRICT_JSON_WRITE,
  )
  return normalizeBase(raw.base ?? {})
}

/** H-25 复制：POST /bases/{id}/duplicate，无请求体（不得注入 client_request_id）。 */
export async function duplicateBase(baseId: string): Promise<BaseSummary> {
  const raw = await http.post<RawBaseResult>(
    `/api/v1/bases/${encodeURIComponent(baseId)}/duplicate`,
    undefined,
    STRICT_JSON_WRITE,
  )
  return normalizeBase(raw.base ?? {})
}

/** H-26 删除：DELETE /bases/{id}，body 仅 confirm。 */
export async function deleteBase(baseId: string): Promise<unknown> {
  return http.delete<unknown>(
    `/api/v1/bases/${encodeURIComponent(baseId)}`,
    { confirm: true },
    STRICT_JSON_WRITE,
  )
}

/** H-27 星标切换：POST /bases/{id}/star，body 仅 starred。 */
export async function toggleStar(baseId: string, starred: boolean): Promise<BaseSummary> {
  const raw = await http.post<RawBaseResult>(
    `/api/v1/bases/${encodeURIComponent(baseId)}/star`,
    { starred },
    STRICT_JSON_WRITE,
  )
  return normalizeBase(raw.base ?? {})
}
