// select 渲染器（票 08 起，票 05 适配后端 option_list 与颜色名映射）。
// 需求 4.3 单选行「选项标签（带颜色）」；色值来自字段配置注入，组件不写死颜色。
// 颜色名 → 调色板色值；未知色安全回退中性标签样式；值不在选项列表时显示
// 「已删除的选项」灰色兜底标签（票 04 口径补全，与多选渲染器/编辑器同文案同样式）。
import { defineRenderer } from '../registry'

import m2 from './m2.module.css'
import styles from './select.module.css'

/** 调色板颜色名 → 色值（与字段配置面板 OPTION_COLOR_PALETTE 对齐）。 */
export const OPTION_COLOR_VALUES: Readonly<Record<string, string>> = {
  blue: '#3370ff',
  green: '#2ea121',
  orange: '#ff8800',
  purple: '#7b61ff',
  red: '#f54a45',
  cyan: '#0fc0d9',
  yellow: '#ffc60a',
  pink: '#ff7bde',
  gray: '#8f959e',
  brown: '#a16b47',
}

/** 颜色名/CSS 色值 → 可注入色值；未知名字返回 undefined（走 token 中性样式）。 */
export function optionColorValue(color?: string): string | undefined {
  const candidate = color?.trim()
  if (candidate === undefined || candidate === '') return undefined
  if (/^#|^rgb|^hsl|^var\(/.test(candidate.toLocaleLowerCase())) return candidate
  return OPTION_COLOR_VALUES[candidate.toLocaleLowerCase()]
}

/** 归一选项（M1 形状：id/label/color）。 */
export interface SelectOption {
  readonly id: string
  readonly label: string
  /** 选项色：调色板自动分配、可改（需求 4.3）；缺省走 token 中性标签样式。 */
  readonly color?: string
}

/** 后端 config_map option_list 形状（接口文档 8.0.6）。 */
export interface RawSelectOption {
  readonly id?: string
  readonly name?: string
  readonly color?: string
}

export interface SelectCellConfig {
  /** M1 归一形状。 */
  readonly options?: readonly SelectOption[]
  /** 后端字段 config_map 形状。 */
  readonly option_list?: readonly RawSelectOption[]
  readonly allow_create?: boolean
}

/**
 * 选项 ID 在配置中查不到（已被删除）时的兜底文案（票 04）。
 * 单选/多选渲染器与编辑器共用同一文案（定义在单选基础模块，多选转发导出）。
 */
export const DELETED_OPTION_LABEL = '已删除的选项'

/** 字段配置 → 统一选项列表：优先 M1 `options`，否则适配后端 `option_list`。 */
export function resolveSelectOptions(config?: SelectCellConfig): SelectOption[] {
  if (config?.options !== undefined) {
    return config.options.map((option) => ({
      id: option.id,
      label: option.label,
      color: optionColorValue(option.color),
    }))
  }
  return (config?.option_list ?? []).map((option) => ({
    id: option.id ?? '',
    label: option.name ?? '',
    color: optionColorValue(option.color),
  }))
}

function SelectCell({ value, config }: { readonly value: unknown; readonly config: unknown }) {
  if (value === null || value === undefined || value === '') {
    return <span className={styles.select} />
  }
  const options = resolveSelectOptions((config ?? {}) as SelectCellConfig)
  const option = options.find((candidate) => candidate.id === value)
  if (option === undefined) {
    // 值不在选项列表（选项已被删除，票 04 口径补全）：与多选兜底同口径——
    // 显示「已删除的选项」灰色标签，不显示原始 ID 长串。
    return (
      <span className={styles.select}>
        <span className={`${styles['select__tag']} ${m2.tag} ${m2['tag--missing']}`}>
          {DELETED_OPTION_LABEL}
        </span>
      </span>
    )
  }
  return (
    <span className={styles.select}>
      <span
        className={styles['select__tag']}
        style={
          option.color === undefined
            ? undefined
            : { backgroundColor: option.color, color: 'var(--color-text-on-brand)' }
        }
      >
        {option.label}
      </span>
    </span>
  )
}

/** select 字段渲染器：单选标签（多选复用同一标签样式）。 */
export const SELECT_RENDERER = defineRenderer<string, SelectCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: SelectCell,
})
