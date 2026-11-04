// select 演示渲染器（票 08）：选项标签按配置色渲染（需求 4.3 单选行「选项标签（带颜色）」）。
// 色值来自字段配置注入（调色板分配/可改），组件不写死任何颜色；标签底样式走 token，
// 注入色仅替换背景并配 --color-text-on-brand 前景（对比度由配置调色板的中深色域保证）。
import { defineRenderer } from '../registry'

import styles from './select.module.css'

/** 单选选项（字段配置注入；M1 演示子集，allow_create 等开关为 M2 编辑器语义）。 */
export interface SelectOption {
  readonly id: string
  readonly label: string
  /** 选项色：调色板自动分配、可改（需求 4.3）；缺省走 token 中性标签样式。 */
  readonly color?: string
}

export interface SelectCellConfig {
  readonly options?: readonly SelectOption[]
}

function SelectCell({ value, config }: { readonly value: unknown; readonly config: unknown }) {
  if (value === null || value === undefined || value === '') {
    return <span className={styles.select} />
  }
  // 内部窄化点：config 由字段配置注入（unknown → SelectCellConfig）
  const { options } = (config ?? {}) as SelectCellConfig
  const option = options?.find((candidate) => candidate.id === value)
  if (option === undefined) {
    // 值不在选项列表（如配置已改）：安全显示原始值，不误配标签
    return <span className={`${styles.select} ${styles['select--unknown']}`}>{String(value)}</span>
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

/** select 字段演示渲染器：单选标签（M2 扩展多选时复用标签样式，多标签逐个渲染）。 */
export const SELECT_RENDERER = defineRenderer<string, SelectCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: SelectCell,
})
