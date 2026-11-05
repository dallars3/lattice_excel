// 视图配置原始形状接口（H-46 / H-47）：票 04 仅用于「删除字段后清理失效条件」。
// H-47 需要 {view_config: <protojson 原始对象>}，不能走票 07 的归一化 ViewConfig 模型。
import { http } from '@/core/http'

/** protojson 原始视图配置（字段名/枚举值保持后端形状）。 */
export type RawViewConfig = Record<string, unknown>

interface RawViewConfigResult {
  view_config?: RawViewConfig
}

const STRICT_JSON_WRITE = { attachClientRequestId: false } as const

/** H-46 读取：GET /views/{view_id}/config → {view_config}。 */
export async function fetchViewConfigRaw(viewId: string): Promise<RawViewConfig> {
  const raw = await http.get<RawViewConfigResult>(
    `/api/v1/views/${encodeURIComponent(viewId)}/config`,
  )
  return raw.view_config ?? {}
}

/** H-47 全量写回：PUT /views/{view_id}/config，body 仅 {view_config}。 */
export async function updateViewConfigRaw(
  viewId: string,
  config: RawViewConfig,
): Promise<RawViewConfig> {
  const raw = await http.put<RawViewConfigResult>(
    `/api/v1/views/${encodeURIComponent(viewId)}/config`,
    { view_config: config },
    STRICT_JSON_WRITE,
  )
  return raw.view_config ?? {}
}

export interface ViewConfigCleanup {
  config: RawViewConfig
  changed: boolean
}

function removeFieldFromList(value: unknown, fieldId: string): { list: unknown[]; changed: boolean } {
  if (!Array.isArray(value)) return { list: [], changed: false }
  const list = value.filter((entry) => {
    if (typeof entry === 'string') return entry !== fieldId
    if (typeof entry === 'object' && entry !== null) {
      return (entry as { field_id?: unknown }).field_id !== fieldId
    }
    return true
  })
  return { list, changed: list.length !== value.length }
}

/** 从单份视图配置中移除对目标字段的全部引用（隐藏/冻结/筛选/排序/分组/字段宽度）。 */
export function removeFieldFromViewConfig(
  config: RawViewConfig,
  fieldId: string,
): ViewConfigCleanup {
  const next: RawViewConfig = { ...config }
  let changed = false

  for (const key of ['hidden_field_list', 'frozen_field_list', 'filter_list', 'sort_list', 'group_by_list']) {
    if (!(key in config)) continue
    const result = removeFieldFromList(config[key], fieldId)
    if (result.changed) {
      next[key] = result.list
      changed = true
    }
  }

  const widthMap = config.field_width_map
  if (typeof widthMap === 'object' && widthMap !== null && fieldId in widthMap) {
    const nextMap = { ...(widthMap as Record<string, unknown>) }
    delete nextMap[fieldId]
    next.field_width_map = nextMap
    changed = true
  }

  return { config: changed ? next : config, changed }
}

export interface ViewConfigEntry {
  viewId: string
  config: RawViewConfig
}

export interface ViewConfigBatchCleanup {
  cleaned: Array<ViewConfigEntry & { changed: boolean }>
  affectedViewIds: string[]
}

/** 批量清理多份视图配置，返回受影响（实际删除了引用）的视图 id 列表。 */
export function removeFieldFromViews(
  entries: readonly ViewConfigEntry[],
  fieldId: string,
): ViewConfigBatchCleanup {
  const cleaned = entries.map((entry) => {
    const result = removeFieldFromViewConfig(entry.config, fieldId)
    return { viewId: entry.viewId, config: result.config, changed: result.changed }
  })
  return {
    cleaned,
    affectedViewIds: cleaned.filter((entry) => entry.changed).map((entry) => entry.viewId),
  }
}
