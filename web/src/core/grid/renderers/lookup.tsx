// lookup 渲染器（票 05）：需求 4.3 引用行「被引用字段当前值（多条并列）」。
// {targetType,targetConfig} 由网格按「关联字段 → 目标表 → 目标字段」解析后注入；
// 按被引用字段类型复用既有渲染器，多条 target_value_list 并列渲染。
import { defineRenderer, type CellRendererDef, type CellRendererProps } from '../registry'

import { ATTACHMENT_RENDERER } from './attachment'
import { TEXT_RENDERER } from './text'
import { DATE_RENDERER } from './date'
import { FORMULA_RENDERER } from './formula'
import styles from './m2.module.css'
import { MULTISELECT_RENDERER } from './multiselect'
import { NUMBER_RENDERER } from './number'
import { PERSON_RENDERER } from './person'
import { RELATION_RENDERER } from './relation'
import { SELECT_RENDERER } from './select'
import { URL_RENDERER } from './url'

export interface LookupCellConfig {
  readonly targetType?: string
  readonly targetConfig?: unknown
}

const TARGET_RENDERERS: Readonly<Record<string, CellRendererDef>> = {
  text: TEXT_RENDERER,
  number: NUMBER_RENDERER,
  select: SELECT_RENDERER,
  multiselect: MULTISELECT_RENDERER,
  date: DATE_RENDERER,
  person: PERSON_RENDERER,
  attachment: ATTACHMENT_RENDERER,
  url: URL_RENDERER,
  formula: FORMULA_RENDERER,
  relation: RELATION_RENDERER,
}

/** 多值字段类型：数组本身就是该类型的一个单元格值，不再按 target_value_list 拆项。 */
const SINGLE_CELL_ARRAY_TYPES = new Set(['multiselect', 'attachment', 'relation'])

function resolveTarget(targetType: string): CellRendererDef {
  return TARGET_RENDERERS[targetType] ?? TEXT_RENDERER
}

function LookupCell({ value, config, onOpenRecord }: CellRendererProps) {
  if (value === null || value === undefined) {
    return <span className={styles.lookup} />
  }
  const resolved = (config ?? {}) as LookupCellConfig
  const targetType = resolved.targetType ?? 'text'
  const renderer = resolveTarget(targetType)
  const Target = renderer.component
  const targetConfig = resolved.targetConfig ?? {}

  const values =
    Array.isArray(value) && !SINGLE_CELL_ARRAY_TYPES.has(targetType)
      ? value
      : [value]

  return (
    <span className={styles.lookup}>
      {values.map((item, index) => (
        <span key={index} className={styles.lookup__item}>
          <Target value={item} config={targetConfig} onOpenRecord={onOpenRecord} />
        </span>
      ))}
    </span>
  )
}

/** 引用字段渲染器：按被引用字段类型渲染，多条并列。 */
export const LOOKUP_RENDERER = defineRenderer<unknown, LookupCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: LookupCell,
})
