// 字段结构接口（H-34 ~ H-40、H-92）：事实源 excel_web_api/internal/gateway/structure_routes.go。
// 网关 decodeJSONBody 使用 DisallowUnknownFields，且除 H-50/H-52/H-53 外写接口均不接受 client_request_id；
// H-35 网关只解码 name/type/config_map（description 写入通道未接线，由前端会话内兜底，见票 04 G 项）。
import { http } from '@/core/http'

import type { FieldOptionValue, FieldType } from '../types/field-config'

/** 归一后的字段（protojson：枚举名转小写、config_map 保留普通对象）。 */
export interface FieldRecord {
  id: string
  tableId: string
  name: string
  type: FieldType
  configMap: Record<string, unknown>
  isPrimary: boolean
  position: number
  description: string
}

export interface CreateFieldInput {
  name: string
  type: FieldType
  configMap: Record<string, unknown>
}

export interface FieldConversionResult {
  /** 预览阶段后端不返回字段。 */
  field: FieldRecord | null
  affectedCount: number
  clearedCount: number
}

export interface DeleteFieldImpact {
  recordCount: number
  dependentFormulas: number
  dependentLookups: number
}

interface RawField {
  id?: string
  table_id?: string
  name?: string
  type?: string
  config_map?: Record<string, unknown> | null
  is_primary?: boolean
  position?: number
  description?: string
}

interface RawFieldList {
  field_list?: RawField[]
}

interface RawFieldResult {
  field?: RawField
}

interface RawConvertResult {
  field?: RawField
  affected_count?: number
  cleared_count?: number
}

interface RawDeleteResult {
  record_count?: number
  dependent_formulas?: number
  dependent_lookups?: number
}

const STRICT_JSON_WRITE = { attachClientRequestId: false } as const

const ENUM_TO_TYPE: Readonly<Record<string, FieldType>> = {
  FIELD_TYPE_TEXT: 'text',
  FIELD_TYPE_NUMBER: 'number',
  FIELD_TYPE_SELECT: 'select',
  FIELD_TYPE_MULTISELECT: 'multiselect',
  FIELD_TYPE_DATE: 'date',
  FIELD_TYPE_PERSON: 'person',
  FIELD_TYPE_ATTACHMENT: 'attachment',
  FIELD_TYPE_URL: 'url',
  FIELD_TYPE_FORMULA: 'formula',
  FIELD_TYPE_RELATION: 'relation',
  FIELD_TYPE_LOOKUP: 'lookup',
}

const VALID_TYPES = new Set<string>(Object.values(ENUM_TO_TYPE))

/** 枚举名（FIELD_TYPE_TEXT）或小写名（text）→ 归一类型；未知值按 text 兜底。 */
export function normalizeFieldType(raw: string | undefined): FieldType {
  const value = (raw ?? '').trim().toUpperCase()
  const mapped = ENUM_TO_TYPE[value]
  if (mapped !== undefined) return mapped
  const lowered = value.toLowerCase()
  return VALID_TYPES.has(lowered) ? (lowered as FieldType) : 'text'
}

function normalizeField(raw: RawField): FieldRecord {
  return {
    id: raw.id ?? '',
    tableId: raw.table_id ?? '',
    name: raw.name ?? '',
    type: normalizeFieldType(raw.type),
    configMap: raw.config_map ?? {},
    isPrimary: raw.is_primary ?? false,
    position: raw.position ?? 0,
    description: raw.description ?? '',
  }
}

/** H-34 字段列表：GET /tables/{table_id}/fields → {field_list}。 */
export async function fetchFields(tableId: string): Promise<FieldRecord[]> {
  const raw = await http.get<RawFieldList>(
    `/api/v1/tables/${encodeURIComponent(tableId)}/fields`,
  )
  return (raw.field_list ?? []).map(normalizeField)
}

/** H-35 新建字段：POST /tables/{table_id}/fields，body 仅 name/type/config_map（不可含 description）。 */
export async function createField(
  tableId: string,
  input: CreateFieldInput,
): Promise<FieldRecord> {
  const raw = await http.post<RawFieldResult>(
    `/api/v1/tables/${encodeURIComponent(tableId)}/fields`,
    {
      name: input.name.trim(),
      type: input.type,
      config_map: input.configMap,
    },
    STRICT_JSON_WRITE,
  )
  return normalizeField(raw.field ?? {})
}

/** H-36 重命名字段：PATCH /fields/{field_id}，body 仅 name。 */
export async function renameField(fieldId: string, name: string): Promise<FieldRecord> {
  const raw = await http.patch<RawFieldResult>(
    `/api/v1/fields/${encodeURIComponent(fieldId)}`,
    { name: name.trim() },
    STRICT_JSON_WRITE,
  )
  return normalizeField(raw.field ?? {})
}

/** H-37 更新字段配置：PUT /fields/{field_id}/config，body 仅 config_map。 */
export async function updateFieldConfig(
  fieldId: string,
  configMap: Record<string, unknown>,
): Promise<FieldRecord> {
  const raw = await http.put<RawFieldResult>(
    `/api/v1/fields/${encodeURIComponent(fieldId)}/config`,
    { config_map: configMap },
    STRICT_JSON_WRITE,
  )
  return normalizeField(raw.field ?? {})
}

/**
 * H-38 类型转换：POST /fields/{field_id}/convert。
 * confirm=false 只取预览（affected_count/cleared_count）；confirm=true 提交并返回字段。
 */
export async function convertField(
  fieldId: string,
  targetType: FieldType,
  confirm: boolean,
  configMap?: Record<string, unknown>,
): Promise<FieldConversionResult> {
  const body: Record<string, unknown> = { target_type: targetType, confirm }
  if (configMap !== undefined) body.config_map = configMap
  const raw = await http.post<RawConvertResult>(
    `/api/v1/fields/${encodeURIComponent(fieldId)}/convert`,
    body,
    STRICT_JSON_WRITE,
  )
  return {
    field: raw.field === undefined ? null : normalizeField(raw.field),
    affectedCount: raw.affected_count ?? 0,
    clearedCount: raw.cleared_count ?? 0,
  }
}

/** H-39 删除字段：DELETE /fields/{field_id}，网关强制 body {confirm:true}（无删除前预览通道）。 */
export async function deleteField(fieldId: string): Promise<DeleteFieldImpact> {
  const raw = await http.delete<RawDeleteResult>(
    `/api/v1/fields/${encodeURIComponent(fieldId)}`,
    { confirm: true },
    STRICT_JSON_WRITE,
  )
  return {
    recordCount: raw.record_count ?? 0,
    dependentFormulas: raw.dependent_formulas ?? 0,
    dependentLookups: raw.dependent_lookups ?? 0,
  }
}

/** H-40 选项维护：PUT /fields/{field_id}/options，body option_list + allow_create。 */
export async function updateFieldOptions(
  fieldId: string,
  options: ReadonlyArray<Partial<FieldOptionValue> & { name: string; color: string }>,
  allowCreate: boolean,
): Promise<FieldRecord> {
  const raw = await http.put<RawFieldResult>(
    `/api/v1/fields/${encodeURIComponent(fieldId)}/options`,
    {
      option_list: options.map((option) => {
        const entry: Record<string, string> = { name: option.name, color: option.color }
        if (option.id !== undefined && option.id !== '') entry.id = option.id
        return entry
      }),
      allow_create: allowCreate,
    },
    STRICT_JSON_WRITE,
  )
  return normalizeField(raw.field ?? {})
}

/** H-92 复制字段（仅配置不复制数据）：POST /fields/{field_id}/duplicate，无请求体。 */
export async function duplicateField(fieldId: string): Promise<FieldRecord> {
  const raw = await http.post<RawFieldResult>(
    `/api/v1/fields/${encodeURIComponent(fieldId)}/duplicate`,
    undefined,
    STRICT_JSON_WRITE,
  )
  return normalizeField(raw.field ?? {})
}
