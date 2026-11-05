// 视图分组呈现（票 07，纯逻辑）：N1 裁定——分组字段作第一排序键，出参平铺；
// 组头仅组名 + 折叠控件，不携带全量记录数。
import type { PersonDirectory } from '@/features/records/metadata'
import { formatNumber, type NumberCellConfig } from '@/core/grid'

import type { FieldRecord } from './api/field'
import type { RecordRow } from '@/features/records/api'
import type { ViewGroupGranularity } from './types/view-config'

const EMPTY_GROUP_KEY = '__empty__'

export interface VisualGroupRow {
  readonly kind: 'group'
  readonly key: string
  readonly label: string
  readonly collapsed: boolean
}

export interface VisualRecordRow {
  readonly kind: 'record'
  readonly record: RecordRow
  readonly recordIndex: number
}

export type VisualRow = VisualGroupRow | VisualRecordRow

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function asList(value: unknown): unknown[] {
  if (value === null || value === undefined || value === '') return []
  return Array.isArray(value) ? value : [value]
}

function dateParts(value: unknown): Date | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

/** 日期桶键（与后端 granularity 语义对齐：day/week/month）。 */
function dateBucket(value: unknown, granularity: ViewGroupGranularity): string | null {
  const date = dateParts(value)
  if (date === null) return null
  const year = date.getFullYear()
  const month = date.getMonth()
  const day = date.getDate()
  if (granularity === 'month') return `${year}-${pad2(month + 1)}`
  if (granularity === 'week') {
    const mondayOffset = (date.getDay() + 6) % 7
    const start = new Date(year, month, day - mondayOffset)
    return `${start.getFullYear()}-${pad2(start.getMonth() + 1)}-${pad2(start.getDate())}`
  }
  return `${year}-${pad2(month + 1)}-${pad2(day)}`
}

function dateBucketLabel(key: string, granularity: ViewGroupGranularity): string {
  const [year, month, day] = key.split('-')
  if (granularity === 'month') return `${year} 年 ${Number(month)} 月`
  if (granularity === 'week') return `${key} 周`
  return `${year}-${month}-${day}`
}

function optionsOf(field: FieldRecord): Array<{ id: string; name: string }> {
  const raw = field.configMap.option_list
  if (!Array.isArray(raw)) return []
  return raw
    .map((entry) => {
      const record = isRecord(entry) ? entry : {}
      return { id: String(record.id ?? ''), name: String(record.name ?? '') }
    })
    .filter((option) => option.id !== '')
}

function optionLabel(field: FieldRecord, id: string): string {
  const matched = optionsOf(field).find((option) => option.id === id)
  return matched?.name ?? id
}

function personLabel(personDirectory: PersonDirectory, userId: string): string {
  return personDirectory[userId]?.name ?? userId
}

function displayText(value: unknown, field: FieldRecord, personDirectory: PersonDirectory): string {
  if (field.type === 'select') {
    const id = String(asList(value)[0] ?? '')
    return optionLabel(field, id)
  }
  if (field.type === 'multiselect') {
    return asList(value)
      .map((item) => optionLabel(field, String(item)))
      .join('、')
  }
  if (field.type === 'person') {
    return asList(value)
      .map((item) => personLabel(personDirectory, String(item)))
      .join('、')
  }
  if (field.type === 'number') {
    return formatNumber(
      typeof value === 'number' ? value : Number(value),
      field.configMap as NumberCellConfig,
    )
  }
  return String(value)
}

/**
 * 分组键：空值统一归入「空」；日期按日/周/月分桶；多值按 id 顺序连接。
 * 同键记录必然相邻（后端以分组字段作第一排序键）。
 */
export function groupKeyForValue(
  value: unknown,
  field: FieldRecord,
  granularity: ViewGroupGranularity | undefined,
): string {
  const list = asList(value)
  if (list.length === 0) return EMPTY_GROUP_KEY
  if (field.type === 'date') {
    const bucket = dateBucket(list[0], granularity ?? 'day')
    return bucket === null ? EMPTY_GROUP_KEY : bucket
  }
  return list.map((item) => String(item)).join('\u0000')
}

/** 组头文案：选项名/人员名/日期桶/「空」；不含记录数。 */
export function groupLabelForValue(
  value: unknown,
  field: FieldRecord,
  granularity: ViewGroupGranularity | undefined,
  personDirectory: PersonDirectory,
): string {
  const list = asList(value)
  if (list.length === 0) return '空'
  if (field.type === 'date') {
    const bucket = dateBucket(list[0], granularity ?? 'day')
    return bucket === null ? '空' : dateBucketLabel(bucket, granularity ?? 'day')
  }
  const text = displayText(value, field, personDirectory)
  return text === '' ? '空' : text
}

/** 平铺记录 → 可视化行（组头 + 组内数据行；折叠组隐藏其数据行）。 */
export function buildViewRows(options: {
  rows: readonly RecordRow[]
  group: { fieldId: string; granularity?: ViewGroupGranularity } | null
  fields: readonly FieldRecord[]
  personDirectory: PersonDirectory
  collapsedKeys: ReadonlySet<string>
}): VisualRow[] {
  const { rows, group, fields, personDirectory, collapsedKeys } = options
  if (group === null) {
    return rows.map((record, recordIndex) => ({ kind: 'record', record, recordIndex }))
  }
  const field = fields.find((candidate) => candidate.id === group.fieldId)
  if (field === undefined) {
    return rows.map((record, recordIndex) => ({ kind: 'record', record, recordIndex }))
  }

  const visual: VisualRow[] = []
  let currentKey: string | null = null
  for (let recordIndex = 0; recordIndex < rows.length; recordIndex += 1) {
    const record = rows[recordIndex]
    const value = record.cellsMap[group.fieldId]
    const key = groupKeyForValue(value, field, group.granularity)
    if (key !== currentKey) {
      currentKey = key
      visual.push({
        kind: 'group',
        key,
        label: groupLabelForValue(value, field, group.granularity, personDirectory),
        collapsed: collapsedKeys.has(key),
      })
    }
    if (!collapsedKeys.has(key)) {
      visual.push({ kind: 'record', record, recordIndex })
    }
  }
  return visual
}

export { EMPTY_GROUP_KEY }
