// formula 渲染器（票 05）：需求 4.3 公式行「计算结果；计算中显示占位；#ERROR hover 原因」。
// 错误哨兵字符串为 `#ERROR:<原因>`（无原因 `#ERROR`）；null 表示尚未算完，显示占位而非 0。
import { defineRenderer } from '../registry'

import styles from './m2.module.css'

export const FORMULA_PLACEHOLDER = '计算中…'
export const FORMULA_ERROR_SENTINEL = '#ERROR'

export interface FormulaDisplay {
  readonly text: string
  readonly reason: string | null
  readonly error: boolean
}

function displayText(value: unknown): string {
  if (value === null || value === undefined) return FORMULA_PLACEHOLDER
  if (typeof value === 'string') return value
  return String(value)
}

/** 公式结果 → 展示文本 / 错误原因。 */
export function parseFormulaValue(value: unknown): FormulaDisplay {
  const text = displayText(value)
  if (!text.startsWith(FORMULA_ERROR_SENTINEL)) {
    return { text, reason: null, error: false }
  }
  const separator = text.indexOf(':')
  const reason = separator === -1 ? FORMULA_ERROR_SENTINEL : text.slice(separator + 1).trim()
  return {
    text: FORMULA_ERROR_SENTINEL,
    reason: reason === '' ? FORMULA_ERROR_SENTINEL : reason,
    error: true,
  }
}

function FormulaCell({ value }: { readonly value: unknown; readonly config: unknown }) {
  const display = parseFormulaValue(value)
  return (
    <span
      className={display.error ? `${styles.formula} ${styles['formula--error']}` : styles.formula}
      title={display.reason ?? (display.text === FORMULA_PLACEHOLDER ? undefined : display.text)}
      data-error={display.error ? 'true' : undefined}
    >
      {display.text}
    </span>
  )
}

/** 公式字段渲染器：只读结果文本，错误哨兵归一为 #ERROR。 */
export const FORMULA_RENDERER = defineRenderer<unknown, unknown>({
  align: 'left',
  clip: 'ellipsis',
  component: FormulaCell,
})
