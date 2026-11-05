// 字段影响面与名称校验（纯逻辑）：删除前依赖推导、标记位与容量判定。
import type { FieldRecord } from '../../api/field'
import {
  FIELD_CREATE_LIMIT_MESSAGE,
  FIELD_LIMIT_MESSAGE,
  MAX_FIELDS_PER_TABLE,
} from '../../capacity'

export { FIELD_CREATE_LIMIT_MESSAGE, FIELD_LIMIT_MESSAGE, MAX_FIELDS_PER_TABLE }

export const MAX_FIELD_NAME_LENGTH = 30
export const MAX_FIELD_DESCRIPTION_LENGTH = 500

export interface DeleteFieldImpactView {
  recordCount: number
  hasMoreRecords: boolean
  dependentFormulas: number
  dependentLookups: number
}

/** 公式表达式以 `{字段名}` 引用该字段（同行引用）。 */
export function countDependentFormulas(
  fields: readonly FieldRecord[],
  target: FieldRecord,
): number {
  return fields.filter((field) => {
    if (field.type !== 'formula' || field.id === target.id) return false
    const expression = field.configMap.expression
    return typeof expression === 'string' && expression.includes(`{${target.name}}`)
  }).length
}

/** 引用字段的 relation_field_id / target_field_id 指向该字段。 */
export function countDependentLookups(
  fields: readonly FieldRecord[],
  target: FieldRecord,
): number {
  return fields.filter((field) => {
    if (field.type !== 'lookup' || field.id === target.id) return false
    return (
      field.configMap.relation_field_id === target.id ||
      field.configMap.target_field_id === target.id
    )
  }).length
}

/** 删除影响面：记录数取当前已加载条数（H-39 无删除前预览通道的兜底口径）。 */
export function buildDeleteImpact(
  fields: readonly FieldRecord[],
  target: FieldRecord,
  loadedRecordCount: number,
  hasMoreRecords: boolean,
): DeleteFieldImpactView {
  return {
    recordCount: loadedRecordCount,
    hasMoreRecords,
    dependentFormulas: countDependentFormulas(fields, target),
    dependentLookups: countDependentLookups(fields, target),
  }
}

/** 字段名 1–30 且同表内不可重名；返回错误文案或 null。 */
export function validateFieldName(
  name: string,
  fields: readonly FieldRecord[],
  exceptFieldId?: string,
): string | null {
  const candidate = name.trim()
  const length = Array.from(candidate).length
  if (length < 1 || length > MAX_FIELD_NAME_LENGTH) {
    return `字段名称需为 1–${MAX_FIELD_NAME_LENGTH} 个字符`
  }
  const duplicated = fields.some(
    (field) => field.id !== exceptFieldId && field.name.trim() === candidate,
  )
  if (duplicated) return '同表内字段名称不可重复'
  return null
}

/** 字段数是否已达单表 100 上限。 */
export function isFieldCapacityReached(fieldCount: number): boolean {
  return fieldCount >= MAX_FIELDS_PER_TABLE
}
