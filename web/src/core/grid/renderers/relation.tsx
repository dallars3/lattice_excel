// relation 渲染器（票 05）：需求 4.3 单向关联行「目标记录主字段值标签（可跳转）」。
// 值为 record_id 或 record_id_list；{labels} 映射（record_id → 目标主字段值）由网格元数据注入，
// 未知 id 回退显示 id；点击回调由网格注入（不再直接持有路由）。
import { defineRenderer, type CellRendererProps } from '../registry'

import styles from './m2.module.css'

export interface RelationCellConfig {
  readonly target_table_id?: string
  readonly allow_multiple?: boolean
  /** record_id → 目标记录主字段值。 */
  readonly labels?: Readonly<Record<string, string>>
}

function asIds(value: unknown): string[] {
  const list = Array.isArray(value) ? value : [value]
  return list
    .filter((item) => item !== null && item !== undefined && item !== '')
    .map((item) => String(item))
}

function RelationCell({ value, config, onOpenRecord }: CellRendererProps) {
  const ids = asIds(value)
  if (ids.length === 0) {
    return <span className={styles.relation} />
  }
  const labels = ((config ?? {}) as RelationCellConfig).labels ?? {}
  return (
    <span className={styles.relation}>
      {ids.map((recordId, index) => {
        const label = labels[recordId] ?? recordId
        return onOpenRecord === undefined ? (
          <span key={`${recordId}-${index}`} className={styles.relation__label} title={label}>
            {label}
          </span>
        ) : (
          <button
            key={`${recordId}-${index}`}
            type="button"
            className={styles.relation__link}
            title={`打开关联记录：${label}`}
            onClick={() => onOpenRecord(recordId)}
          >
            {label}
          </button>
        )
      })}
    </span>
  )
}

/** 关联字段渲染器：目标记录主字段值标签，可点击跳转。 */
export const RELATION_RENDERER = defineRenderer<string, RelationCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: RelationCell,
})
