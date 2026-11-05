// 视图配置编解码（票 07，纯函数）：protojson 真实形状 ↔ 内部模型。
// - protojson：filter_list{field_id,op:"VIEW_FILTER_OP_*",value:<common.Value>}、
//   sort_list{field_id,dir:"SORT_DIR_ASC|DESC"}、group_by_list{field_id,granularity?}、
//   row_height:"ROW_HEIGHT_*"、field_width_map{field_id:number}。
// - filter value 复用票 05 的 common.Value 解码（int64 为字符串）；空操作省略 value。
import { decodeCellValue } from '@/features/records/cell-codec'

import type { FieldType } from './field-config'
import {
  clampFieldWidth,
  createEmptyViewConfig,
  DEFAULT_VIEW_ROW_HEIGHT,
  type ViewConfig,
  type ViewFilter,
  type ViewFilterOp,
  type ViewGroup,
  type ViewGroupGranularity,
  type ViewRowHeight,
  type ViewSort,
  type ViewSortDir,
} from './view-config'
import { completeFilters, filterOpNeedsValue } from './view-config-validation'

type RawRecord = Record<string, unknown>

const FILTER_OPS: readonly ViewFilterOp[] = [
  'contains',
  'not_contains',
  'is_empty',
  'not_empty',
  'eq',
  'ne',
  'gt',
  'lt',
  'between',
  'in',
  'not_in',
  'contains_any',
  'contains_all',
  'before',
  'after',
  'today',
  'has',
  'none',
]

function isRecord(value: unknown): value is RawRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function asStringList(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string' && item !== '')
}

function decodeFilterOp(raw: unknown, fallback: ViewFilterOp = 'contains'): ViewFilterOp {
  const value = String(raw ?? '').trim()
  if (value === '') return fallback
  const normalized = value.replace(/^VIEW_FILTER_OP_/, '').toLowerCase()
  return FILTER_OPS.includes(normalized as ViewFilterOp)
    ? (normalized as ViewFilterOp)
    : fallback
}

function encodeFilterOp(op: ViewFilterOp): string {
  return `VIEW_FILTER_OP_${op.toUpperCase()}`
}

function decodeSortDir(raw: unknown): ViewSortDir {
  return String(raw ?? '').toUpperCase().replace(/^SORT_DIR_/, '') === 'DESC' ? 'desc' : 'asc'
}

function encodeSortDir(dir: ViewSortDir): string {
  return `SORT_DIR_${dir.toUpperCase()}`
}

function decodeGranularity(raw: unknown): ViewGroupGranularity | undefined {
  const normalized = String(raw ?? '').trim().replace(/^GRANULARITY_/, '').toLowerCase()
  if (normalized === 'month' || normalized === 'week' || normalized === 'day') {
    return normalized
  }
  return undefined
}

function encodeGranularity(granularity: ViewGroupGranularity): string {
  return `GRANULARITY_${granularity.toUpperCase()}`
}

function decodeRowHeight(raw: unknown): ViewRowHeight {
  const normalized = String(raw ?? '').trim().replace(/^ROW_HEIGHT_/, '').toLowerCase()
  if (normalized === 'compact' || normalized === 'medium' || normalized === 'tall') {
    return normalized
  }
  return DEFAULT_VIEW_ROW_HEIGHT
}

function encodeRowHeight(rowHeight: ViewRowHeight): string {
  return `ROW_HEIGHT_${rowHeight.toUpperCase()}`
}

function decodeFilters(raw: unknown): ViewFilter[] {
  if (!Array.isArray(raw)) return []
  const filters: ViewFilter[] = []
  for (const entry of raw) {
    if (!isRecord(entry)) continue
    const fieldId = String(entry.field_id ?? '')
    if (fieldId === '') continue
    const op = decodeFilterOp(entry.op)
    const value = filterOpNeedsValue(op) ? decodeCellValue(entry.value) : null
    filters.push({ fieldId, op, value })
  }
  return filters
}

function decodeSorts(raw: unknown): ViewSort[] {
  if (!Array.isArray(raw)) return []
  const sorts: ViewSort[] = []
  for (const entry of raw) {
    if (!isRecord(entry)) continue
    const fieldId = String(entry.field_id ?? '')
    if (fieldId === '') continue
    sorts.push({ fieldId, dir: decodeSortDir(entry.dir) })
  }
  return sorts
}

function decodeGroup(raw: unknown): ViewGroup | null {
  if (!Array.isArray(raw) || raw.length === 0) return null
  const first = raw[0]
  if (!isRecord(first)) return null
  const fieldId = String(first.field_id ?? '')
  if (fieldId === '') return null
  const granularity = decodeGranularity(first.granularity)
  return granularity === undefined ? { fieldId } : { fieldId, granularity }
}

function decodeFieldWidthMap(raw: unknown): Record<string, number> {
  if (!isRecord(raw)) return {}
  const map: Record<string, number> = {}
  for (const [fieldId, value] of Object.entries(raw)) {
    if (fieldId === '') continue
    const width = typeof value === 'number' ? value : Number(value)
    if (!Number.isFinite(width)) continue
    map[fieldId] = clampFieldWidth(width)
  }
  return map
}

/** protojson view_config → 内部模型（缺省值按 UI 基线）。 */
export function decodeViewConfig(raw: unknown): ViewConfig {
  if (!isRecord(raw)) return createEmptyViewConfig()
  return {
    hiddenFieldIds: asStringList(raw.hidden_field_list),
    frozenFieldIds: asStringList(raw.frozen_field_list),
    filters: decodeFilters(raw.filter_list),
    sorts: decodeSorts(raw.sort_list),
    group: decodeGroup(raw.group_by_list),
    rowHeight: decodeRowHeight(raw.row_height),
    fieldWidthMap: decodeFieldWidthMap(raw.field_width_map),
  }
}

function encodeCommonValue(value: unknown): RawRecord | undefined {
  if (value === null || value === undefined) return undefined
  if (Array.isArray(value)) {
    const items = value.map(encodeCommonValue).filter((item): item is RawRecord => item !== undefined)
    return { list_value: { value_list: items } }
  }
  if (typeof value === 'string') return { string_value: value }
  if (typeof value === 'boolean') return { bool_value: value }
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) return undefined
    return Number.isInteger(value)
      ? { int_value: String(value) }
      : { double_value: value }
  }
  if (isRecord(value)) {
    const nested = Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, encodeCommonValue(item)]),
    )
    return nested as RawRecord
  }
  return { string_value: String(value) }
}

function encodeFilters(filters: readonly ViewFilter[]): RawRecord[] {
  // 未填完的条件不上送：后端对需要 value 的 op 收到 nil 会直接判 false（网格 0 条）。
  return completeFilters(filters).map((filter) => {
    const entry: RawRecord = {
      field_id: filter.fieldId,
      op: encodeFilterOp(filter.op),
    }
    if (filterOpNeedsValue(filter.op)) {
      const value = encodeCommonValue(filter.value)
      if (value !== undefined) entry.value = value
    }
    return entry
  })
}

function encodeSorts(sorts: readonly ViewSort[]): RawRecord[] {
  return sorts.map((sort) => ({
    field_id: sort.fieldId,
    dir: encodeSortDir(sort.dir),
  }))
}

function encodeGroup(group: ViewGroup | null): RawRecord[] {
  if (group === null || group.fieldId === '') return []
  const entry: RawRecord = { field_id: group.fieldId }
  if (group.granularity !== undefined) {
    entry.granularity = encodeGranularity(group.granularity)
  }
  return [entry]
}

function encodeFieldWidthMap(map: Readonly<Record<string, number>>): Record<string, number> {
  const encoded: Record<string, number> = {}
  for (const [fieldId, width] of Object.entries(map)) {
    if (fieldId === '') continue
    encoded[fieldId] = clampFieldWidth(width)
  }
  return encoded
}

/** 内部模型 → protojson view_config（全量替换载荷）。 */
export function encodeViewConfig(config: ViewConfig): RawRecord {
  return {
    hidden_field_list: [...config.hiddenFieldIds],
    frozen_field_list: [...config.frozenFieldIds],
    filter_list: encodeFilters(config.filters),
    sort_list: encodeSorts(config.sorts),
    group_by_list: encodeGroup(config.group),
    row_height: encodeRowHeight(config.rowHeight),
    field_width_map: encodeFieldWidthMap(config.fieldWidthMap),
  }
}

/**
 * 记录查询的数据条件签名（筛选/排序/分组）。
 * 作为 H-48 查询 key 的一部分：条件变更即新 key，从空游标全量重拉，不拼接旧页。
 */
export function viewDataSignature(config: ViewConfig | null): string {
  if (config === null) return ''
  // 仅完整条件参与签名：未填完的条件不应改变查询 key（否则会触发无谓重拉/空结果）。
  const filters = completeFilters(config.filters)
  // 无完整筛选/排序/分组时与「配置未加载」共用空签名，避免配置到达后无谓重拉记录。
  if (filters.length === 0 && config.sorts.length === 0 && config.group === null) return ''
  return JSON.stringify({
    filters: filters.map((filter) => [filter.fieldId, filter.op, filter.value]),
    sorts: config.sorts.map((sort) => [sort.fieldId, sort.dir]),
    group: config.group === null ? null : [config.group.fieldId, config.group.granularity ?? null],
  })
}

/** 字段类型是否可参与筛选（lookup 无白名单）。 */
export function isFilterableFieldType(fieldType: FieldType): boolean {
  return fieldType !== 'lookup'
}
