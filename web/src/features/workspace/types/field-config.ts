// 字段类型与配置（接口文档 §8.0.4 / §8.5.5）：类型枚举、默认配置、主字段限制、转换矩阵与选项裁决。
// 纯逻辑模块，不依赖 React / 网络；供 api、字段面板与网格表头共用。

/** 11 种字段类型（protojson 枚举名归一为小写）。 */
export type FieldType =
  | 'text'
  | 'number'
  | 'select'
  | 'multiselect'
  | 'date'
  | 'person'
  | 'attachment'
  | 'url'
  | 'formula'
  | 'relation'
  | 'lookup'

export interface FieldOptionValue {
  id: string
  name: string
  color: string
}

/** 新增字段面板的类型选项（含图标语义说明）。 */
export interface FieldTypeOption {
  type: FieldType
  label: string
  hint: string
}

export const ALL_FIELD_TYPES: readonly FieldType[] = [
  'text',
  'number',
  'select',
  'multiselect',
  'date',
  'person',
  'attachment',
  'url',
  'formula',
  'relation',
  'lookup',
]

export const FIELD_TYPE_OPTIONS: readonly FieldTypeOption[] = [
  { type: 'text', label: '多行文本', hint: '单行省略展示，编辑时支持换行' },
  { type: 'number', label: '数字', hint: '支持整数、小数、百分比与货币格式' },
  { type: 'select', label: '单选', hint: '从选项列表中选一个，带颜色标签' },
  { type: 'multiselect', label: '多选', hint: '从选项列表中选择多个' },
  { type: 'date', label: '日期', hint: '可选择是否包含时间与三种展示格式' },
  { type: 'person', label: '人员', hint: '选择 Base 协作者，可设为多选' },
  { type: 'attachment', label: '附件', hint: '上传文件并以缩略图展示' },
  { type: 'url', label: '超链接', hint: '仅接受 http/https 地址' },
  { type: 'formula', label: '公式', hint: '按表达式计算，只读展示结果' },
  { type: 'relation', label: '单向关联', hint: '关联本 Base 内其他数据表的记录' },
  { type: 'lookup', label: '引用', hint: '基于关联字段取目标字段的当前值' },
]

/** 主字段允许类型（需求 §4.3：多行文本/数字/单选/公式）。 */
export const PRIMARY_FIELD_TYPES: readonly FieldType[] = ['text', 'number', 'select', 'formula']

/** 选项颜色调色板名（票 05 渲染器据此映射颜色）。 */
export const OPTION_COLOR_PALETTE = [
  'blue',
  'green',
  'orange',
  'purple',
  'red',
  'cyan',
  'yellow',
  'pink',
  'gray',
  'brown',
] as const

export type OptionColorName = (typeof OPTION_COLOR_PALETTE)[number]

export const NUMBER_FORMATS = ['int', 'decimal', 'percent', 'currency'] as const
export const NUMBER_CURRENCIES = ['¥', '$', '€', '£'] as const
export const DATE_FORMATS = ['YYYY/MM/DD', 'YYYY-MM-DD', 'YYYY年M月D日'] as const

/** 主字段类型限制判定。 */
export function isPrimaryFieldType(type: FieldType): boolean {
  return PRIMARY_FIELD_TYPES.includes(type)
}

/** 公式/关联/引用三类不参与任何转换方向（接口文档 §8.5.5）。 */
export function isConvertExcludedType(type: FieldType): boolean {
  return type === 'formula' || type === 'relation' || type === 'lookup'
}

/** 选项类字段（单选/多选）。 */
export function isOptionFieldType(type: FieldType): type is 'select' | 'multiselect' {
  return type === 'select' || type === 'multiselect'
}

const CONVERT_MATRIX: Readonly<Record<FieldType, readonly FieldType[]>> = {
  text: ['number', 'select', 'multiselect', 'date'],
  number: ['text'],
  select: ['multiselect', 'text'],
  multiselect: ['select', 'text'],
  date: ['text'],
  person: ['text'],
  attachment: ['text'],
  url: ['text'],
  formula: [],
  relation: [],
  lookup: [],
}

/** 源类型允许转换的目标类型（不含自身与公式/关联/引用）。 */
export function allowedConvertTargets(type: FieldType): FieldType[] {
  return [...CONVERT_MATRIX[type]]
}

/** 各类型默认 config（与后端 §8.0.4 参数键一致；无配置类型为空对象）。 */
export function defaultFieldConfig(type: FieldType): Record<string, unknown> {
  switch (type) {
    case 'number':
      return { format: 'int', decimals: 0, currency: '¥', thousands: false }
    case 'select':
    case 'multiselect':
      return { option_list: [], allow_create: false }
    case 'date':
      return { include_time: false, format: 'YYYY-MM-DD' }
    case 'person':
      return { allow_multiple: false }
    case 'formula':
      return { expression: '' }
    case 'relation':
      return { target_table_id: '', allow_multiple: false }
    case 'lookup':
      return { relation_field_id: '', target_field_id: '' }
    default:
      return {}
  }
}

/** 调色板顺序分配：取第一个未被占用的颜色，用尽后按已有数量循环。 */
export function nextOptionColor(existing: readonly string[]): OptionColorName {
  const used = new Set(existing)
  const available = OPTION_COLOR_PALETTE.find((color) => !used.has(color))
  if (available !== undefined) return available
  return OPTION_COLOR_PALETTE[existing.length % OPTION_COLOR_PALETTE.length]
}

/** 选项录入裁决结果：matched=已有选项；created=待新建选项（id 为空，保存时由后端生成）。 */
export interface OptionResolution {
  matched: FieldOptionValue | null
  created: FieldOptionValue | null
  message: string | null
}

export const NO_MATCH_OPTION_MESSAGE = '无匹配选项，请联系表格管理员添加'

/** 录入值 → 选项（allow_create 关闭时禁止新建并给出联系管理员提示，D14）。 */
export function resolveOptionEntry(
  allowCreate: boolean,
  options: readonly FieldOptionValue[],
  input: string,
): OptionResolution {
  const candidate = input.trim()
  if (candidate === '') return { matched: null, created: null, message: null }

  const lowered = candidate.toLocaleLowerCase()
  const matched = options.find((option) => option.name.trim().toLocaleLowerCase() === lowered)
  if (matched !== undefined) return { matched, created: null, message: null }

  if (!allowCreate) return { matched: null, created: null, message: NO_MATCH_OPTION_MESSAGE }

  return {
    matched: null,
    created: {
      id: '',
      name: candidate,
      color: nextOptionColor(options.map((option) => option.color)),
    },
    message: null,
  }
}
