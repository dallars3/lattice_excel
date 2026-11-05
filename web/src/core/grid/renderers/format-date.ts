// formatDate 纯函数（票 05）：需求 4.3 日期字段三种展示格式 + 是否含时间。
// 不引入日期库：本地时区解析毫秒时间戳，输出手算可断言的固定文本；
// 最小日期 1900-01-01 仅约束编辑器（票 06），展示层不截断历史数据。
/** 日期格式（接口文档 §8.0.4）。 */
export type DateFormat = 'YYYY/MM/DD' | 'YYYY-MM-DD' | 'YYYY年M月D日'

export interface DateCellConfig {
  readonly format?: string
  readonly include_time?: boolean
}

const DEFAULT_FORMAT: DateFormat = 'YYYY-MM-DD'

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

function toTimestamp(value: unknown): number | null {
  if (typeof value === 'number') return Number.isFinite(value) ? value : null
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : null
  }
  return null
}

/** 毫秒时间戳 → 展示文本；null/非法值返回空串。 */
export function formatDate(value: unknown, config?: DateCellConfig): string {
  const timestamp = toTimestamp(value)
  if (timestamp === null) return ''
  const date = new Date(timestamp)
  if (Number.isNaN(date.getTime())) return ''

  const year = date.getFullYear()
  const month = date.getMonth() + 1
  const day = date.getDate()
  const format = (config?.format as DateFormat | undefined) ?? DEFAULT_FORMAT

  let text: string
  switch (format) {
    case 'YYYY/MM/DD':
      text = `${year}/${pad2(month)}/${pad2(day)}`
      break
    case 'YYYY年M月D日':
      text = `${year}年${month}月${day}日`
      break
    case 'YYYY-MM-DD':
    default:
      text = `${year}-${pad2(month)}-${pad2(day)}`
      break
  }

  if (config?.include_time === true) {
    return `${text} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`
  }
  return text
}
