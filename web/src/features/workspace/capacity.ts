// 容量上限与提示文案（票 09 收口）：表 50 / 字段 100 / 记录 50,000 / 筛选 5 / 排序 3。
// 各入口（表/字段/记录/视图面板）统一从这里取常量与文案，避免同义提示漂移。

export const MAX_TABLES_PER_BASE = 50
export const MAX_FIELDS_PER_TABLE = 100
export const MAX_RECORDS_PER_TABLE = 50000
export const MAX_FILTERS = 5
export const MAX_SORTS = 3

export const TABLE_LIMIT_MESSAGE = `每个 Base 最多 ${MAX_TABLES_PER_BASE} 张数据表`
export const FIELD_LIMIT_MESSAGE = `字段数已达上限（${MAX_FIELDS_PER_TABLE}），无法新增字段`
export const FIELD_CREATE_LIMIT_MESSAGE = `字段数已达上限（${MAX_FIELDS_PER_TABLE}），请先删除不再使用的字段`
export const RECORD_LIMIT_MESSAGE = '已达单表 5 万行上限，请联系管理者'
export const FILTER_LIMIT_MESSAGE = `筛选条件最多 ${MAX_FILTERS} 条`
export const SORT_LIMIT_MESSAGE = `排序最多 ${MAX_SORTS} 级`
