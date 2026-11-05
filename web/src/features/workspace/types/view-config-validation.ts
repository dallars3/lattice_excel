// 视图配置校验（票 07，纯逻辑）：筛选项 ≤5、排序 ≤3、分组单字段；操作符按字段类型白名单。
// 事实源：接口文档 8.0.5；分组 N1 裁定（仅作第一排序键）。
// 票 09：上限与提示文案统一由 workspace/capacity.ts 提供。
import {
  FILTER_LIMIT_MESSAGE,
  MAX_FILTERS,
  MAX_SORTS,
  SORT_LIMIT_MESSAGE,
} from '../capacity'
import type { FieldType } from './field-config'
import type { ViewConfig, ViewFilter, ViewFilterOp } from './view-config'

export { FILTER_LIMIT_MESSAGE, MAX_FILTERS, MAX_SORTS, SORT_LIMIT_MESSAGE }

const FILTER_OP_OPTIONS: Readonly<Record<FieldType, readonly ViewFilterOp[]>> = {
  text: ['contains', 'not_contains', 'is_empty', 'not_empty'],
  number: ['eq', 'ne', 'gt', 'lt', 'between', 'is_empty'],
  select: ['in', 'not_in', 'is_empty'],
  multiselect: ['contains_any', 'contains_all', 'is_empty'],
  date: ['eq', 'before', 'after', 'between', 'today', 'is_empty'],
  person: ['contains', 'is_empty'],
  attachment: ['has', 'none'],
  relation: ['has', 'none'],
  // 公式按结果类型给出数值/文本通用比较；引用不提供筛选。
  formula: ['eq', 'ne', 'gt', 'lt', 'is_empty'],
  lookup: [],
  // 超链接按文本包含语义处理（接口文档未单列，沿用文本白名单）。
  url: ['contains', 'not_contains', 'is_empty', 'not_empty'],
}

/** 字段类型可用的筛选操作符（lookup 为空 → 该字段不出现在筛选面板）。 */
export function operatorOptionsFor(fieldType: FieldType): ViewFilterOp[] {
  return [...FILTER_OP_OPTIONS[fieldType]]
}

/** 无值操作符（空值判断、今天、有/无）。 */
const VALUE_FREE_OPS: ReadonlySet<ViewFilterOp> = new Set([
  'is_empty',
  'not_empty',
  'today',
  'has',
  'none',
])

export function filterOpNeedsValue(op: ViewFilterOp): boolean {
  return !VALUE_FREE_OPS.has(op)
}

/** 两值操作符（between 起止）。 */
export function filterOpNeedsRange(op: ViewFilterOp): boolean {
  return op === 'between'
}

/** 多值操作符（选项 id 集合）。 */
export function filterOpNeedsList(op: ViewFilterOp): boolean {
  return op === 'in' || op === 'not_in' || op === 'contains_any' || op === 'contains_all'
}

export const FILTER_OP_LABELS: Readonly<Record<ViewFilterOp, string>> = {
  contains: '包含',
  not_contains: '不包含',
  is_empty: '为空',
  not_empty: '不为空',
  eq: '等于',
  ne: '不等于',
  gt: '大于',
  lt: '小于',
  between: '介于',
  in: '属于',
  not_in: '不属于',
  contains_any: '包含任一',
  contains_all: '包含全部',
  before: '早于',
  after: '晚于',
  today: '今天',
  has: '有',
  none: '无',
}

/** 单值是否为「已填写」：null/undefined 与纯空白字符串视为未填。 */
function isPresent(value: unknown): boolean {
  if (value === null || value === undefined) return false
  if (typeof value === 'string') return value.trim() !== ''
  return true
}

/**
 * 筛选条件是否已完整：无值操作符恒完整；between 需两端；in/not_in/contains_* 需非空数组；
 * 其余需 value 非空。不完整的条件不参与编码与数据签名，避免后端 0 条结果。
 */
export function isFilterComplete(filter: ViewFilter): boolean {
  if (!filterOpNeedsValue(filter.op)) return true
  if (filterOpNeedsRange(filter.op)) {
    return Array.isArray(filter.value)
      && filter.value.length === 2
      && isPresent(filter.value[0])
      && isPresent(filter.value[1])
  }
  if (filterOpNeedsList(filter.op)) {
    return Array.isArray(filter.value)
      && filter.value.length > 0
      && filter.value.every(isPresent)
  }
  return isPresent(filter.value)
}

/** 仅保留已完整填写的筛选条件（如选项未选、值未输入时不下发）。 */
export function completeFilters(filters: readonly ViewFilter[]): ViewFilter[] {
  return filters.filter(isFilterComplete)
}

/** 上限校验：返回错误文案数组（空数组即合法）。 */
export function validateViewConfig(config: ViewConfig): string[] {
  const errors: string[] = []
  if (config.filters.length > MAX_FILTERS) errors.push(FILTER_LIMIT_MESSAGE)
  if (config.sorts.length > MAX_SORTS) errors.push(SORT_LIMIT_MESSAGE)
  if (config.group !== null && config.group.fieldId === '') errors.push('分组需选择一个字段')
  return errors
}
