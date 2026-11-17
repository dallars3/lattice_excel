// multiselect 渲染器（票 05）：需求 4.3 多选行「多个标签」，值编码为 option_id 数组。
// 选项配置与单选同源（resolveSelectOptions），逐标签渲染；失效 id（选项已删除）
// 安全显示「已删除的选项」灰色兜底标签，不显示原始 ID 长串（票 04 去乱码；
// 票 04 口径补全起文案常量上移单选基础模块共享，单选渲染器/编辑器同口径）。
import { defineRenderer } from '../registry'

import styles from './select.module.css'
import m2 from './m2.module.css'
import { DELETED_OPTION_LABEL, resolveSelectOptions, type SelectCellConfig } from './select'

// 文案常量上移至 select.tsx（单选/多选共用）；此处转发导出保持既有引用面不变。
export { DELETED_OPTION_LABEL }

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
        const missing = option === undefined
        return (
          <span
            key={`${String(id)}-${index}`}
            className={
              missing
                ? `${styles['select__tag']} ${m2.tag} ${m2['tag--missing']}`
                : `${styles['select__tag']} ${m2.tag}`
            }
            style={
              option?.color === undefined
                ? undefined
                : { backgroundColor: option.color, color: 'var(--color-text-on-brand)' }
            }
          >
            {option?.label ?? DELETED_OPTION_LABEL}
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
