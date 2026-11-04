// number 演示渲染器（票 08）：整数/小数位数/千分位按配置渲染（需求 4.3 数字字段行的 M1 子集，
// 百分比/货币后置 M2）。语义元数据：右对齐 + 单行省略（数字单元格标准语义，技术方案 6.4）。
import { defineRenderer } from '../registry'
import { formatNumber, type NumberCellConfig } from './format-number'

import styles from './number.module.css'

function NumberCell({ value, config }: { readonly value: unknown; readonly config: unknown }) {
  // 内部窄化点：非 number 值按空单元格处理（安全显示，不抛错）
  const text = formatNumber(typeof value === 'number' ? value : null, config as NumberCellConfig | undefined)
  return <span className={styles.number}>{text}</span>
}

/** number 字段演示渲染器：格式化由 formatNumber 纯函数承载（组件只做展示）。 */
export const NUMBER_RENDERER = defineRenderer<number, NumberCellConfig>({
  align: 'right',
  clip: 'ellipsis',
  component: NumberCell,
})
