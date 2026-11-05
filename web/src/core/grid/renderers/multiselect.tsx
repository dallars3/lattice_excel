// multiselect 渲染器（票 05）：需求 4.3 多选行「多个标签」，值编码为 option_id 数组。
// 选项配置与单选同源（resolveSelectOptions），逐标签渲染；未知 id 安全显示原始值。
import { defineRenderer } from '../registry'

import styles from './select.module.css'
import m2 from './m2.module.css'
import { resolveSelectOptions, type SelectCellConfig } from './select'

function asList(value: unknown): unknown[] {
  if (Array.isArray(value)) return value
  if (value === null || value === undefined || value === '') return []
  return [value]
}

function MultiselectCell({ value, config }: { readonly value: unknown; readonly config: unknown }) {
  const ids = asList(value)
  if (ids.length === 0) {
    return <span className={m2.tags} />
  }
  const options = resolveSelectOptions((config ?? {}) as SelectCellConfig)
  return (
    <span className={m2.tags}>
      {ids.map((id, index) => {
        const option = options.find((candidate) => candidate.id === id)
        return (
          <span
            key={`${String(id)}-${index}`}
            className={`${styles['select__tag']} ${m2.tag}`}
            style={
              option?.color === undefined
                ? undefined
                : { backgroundColor: option.color, color: 'var(--color-text-on-brand)' }
            }
          >
            {option?.label ?? String(id)}
          </span>
        )
      })}
    </span>
  )
}

/** 多选字段渲染器：多个带色选项标签并列。 */
export const MULTISELECT_RENDERER = defineRenderer<unknown, SelectCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: MultiselectCell,
})
