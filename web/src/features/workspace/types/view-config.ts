// 视图配置内部模型（票 07）：protojson 形状只在 api/codec 层出现，UI 与网格消费本模型。
// 事实源：接口文档 8.0.5 + excel_web_api/internal/gateway/structure_routes.go（H-46/H-47）。

/** 筛选操作符（接口文档 8.0.5 白名单；枚举名去掉 VIEW_FILTER_OP_ 前缀并小写）。 */
export type ViewFilterOp =
  | 'contains'
  | 'not_contains'
  | 'is_empty'
  | 'not_empty'
  | 'eq'
  | 'ne'
  | 'gt'
  | 'lt'
  | 'between'
  | 'in'
  | 'not_in'
  | 'contains_any'
  | 'contains_all'
  | 'before'
  | 'after'
  | 'today'
  | 'has'
  | 'none'

export interface ViewFilter {
  fieldId: string
  op: ViewFilterOp
  /** 普通值：字符串/数字/选项 id 数组/两值数组；空操作省略为 null。 */
  value: unknown
}

export type ViewSortDir = 'asc' | 'desc'

export interface ViewSort {
  fieldId: string
  dir: ViewSortDir
}

export type ViewGroupGranularity = 'day' | 'week' | 'month'

export interface ViewGroup {
  fieldId: string
  /** 仅日期字段有意义；未选择时不携带。 */
  granularity?: ViewGroupGranularity
}

export type ViewRowHeight = 'compact' | 'medium' | 'tall'

export interface ViewConfig {
  hiddenFieldIds: string[]
  frozenFieldIds: string[]
  filters: ViewFilter[]
  sorts: ViewSort[]
  group: ViewGroup | null
  rowHeight: ViewRowHeight
  fieldWidthMap: Record<string, number>
}

/** 宽度范围（N4 裁定 / 接口文档 8.0.5）。 */
export const MIN_FIELD_WIDTH = 40
export const MAX_FIELD_WIDTH = 2000
/** 未出现在 field_width_map 的字段默认宽度（spec 视图章）。 */
export const DEFAULT_FIELD_WIDTH = 160
/** 默认行高：后端 UNSPECIFIED 时前端按中等展示。 */
export const DEFAULT_VIEW_ROW_HEIGHT: ViewRowHeight = 'medium'

/** 宽度 clamp 到 40–2000（非有限数按默认宽度）。 */
export function clampFieldWidth(width: number): number {
  if (!Number.isFinite(width)) return DEFAULT_FIELD_WIDTH
  return Math.min(MAX_FIELD_WIDTH, Math.max(MIN_FIELD_WIDTH, Math.round(width)))
}

/** 空配置（新建/无配置时网格与面板的基线）。 */
export function createEmptyViewConfig(): ViewConfig {
  return {
    hiddenFieldIds: [],
    frozenFieldIds: [],
    filters: [],
    sorts: [],
    group: null,
    rowHeight: DEFAULT_VIEW_ROW_HEIGHT,
    fieldWidthMap: {},
  }
}
