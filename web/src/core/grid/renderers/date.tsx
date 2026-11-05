// date 渲染器（票 05）：按字段配置的三种格式与是否含时间展示毫秒时间戳；
// 格式化逻辑由纯函数 formatDate 承载，组件只做展示与 title。
import { defineRenderer } from '../registry'

import { formatDate, type DateCellConfig } from './format-date'
import styles from './m2.module.css'

function DateCell({ value, config }: { readonly value: unknown; readonly config: unknown }) {
  const text = formatDate(value, config as DateCellConfig | undefined)
  return (
    <span className={styles.date} title={text === '' ? undefined : text}>
      {text}
    </span>
  )
}

/** 日期字段渲染器：左对齐 + 单行省略。 */
export const DATE_RENDERER = defineRenderer<number, DateCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: DateCell,
})
