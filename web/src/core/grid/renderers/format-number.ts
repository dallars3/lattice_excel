// formatNumber 纯函数（票 08 起，票 05 扩展完整数字格式）。
// 需求 4.3 数字字段行：整数/小数 0-4 位/百分比/货币 ¥$€£、千分位开关、超精度四舍五入、
// 空值与 0 区分。M1 的 `precision` 配置键保留（既有测试与调用方不破坏），
// M2 后端 config_map 的 `format/decimals/currency` 键优先。
/** 数字格式（接口文档 §8.0.4）。 */
export type NumberFormat = 'int' | 'decimal' | 'percent' | 'currency'

/** number 字段类型配置：M1 `precision` 与 M2 `decimals` 兼容。 */
export interface NumberCellConfig {
  /** 整数/小数/百分比/货币；缺省按 M1 行为（precision 小数位，无格式修饰）。 */
  readonly format?: NumberFormat
  /** 小数位数：0-4（后端键）。 */
  readonly decimals?: number
  /** 小数位数（M1 键，兼容旧调用）。 */
  readonly precision?: number
  /** 货币符号（¥/$/€/£）。 */
  readonly currency?: string
  /** 千分位分隔开关。缺省关。 */
  readonly thousands?: boolean
}

const MAX_PRECISION = 4
const THOUSANDS_GROUP = /\B(?=(\d{3})+(?!\d))/g
const DEFAULT_CURRENCY = '¥'

function clampPrecision(value: number): number {
  const truncated = Math.trunc(value)
  if (!Number.isFinite(truncated)) return 0
  return Math.min(Math.max(0, truncated), MAX_PRECISION)
}

function resolveDecimals(config?: NumberCellConfig): number {
  const format = config?.format
  if (format === 'int') return 0
  if (format === 'decimal' || format === 'percent' || format === 'currency') {
    return clampPrecision(config?.decimals ?? config?.precision ?? 2)
  }
  return clampPrecision(config?.decimals ?? config?.precision ?? 0)
}

function groupThousands(text: string): string {
  const [intPart, decimalPart] = text.split('.')
  const grouped = intPart.replace(THOUSANDS_GROUP, ',')
  return decimalPart === undefined ? grouped : `${grouped}.${decimalPart}`
}

/** 数字 → 展示文本：null/undefined/非有限值 → 空串（空值与 0 区分显示）；0 → '0'。 */
export function formatNumber(value: number | null | undefined, config?: NumberCellConfig): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return ''
  }

  const format = config?.format
  const precision = resolveDecimals(config)
  const scaled = format === 'percent' ? value * 100 : value
  let text = scaled.toFixed(precision)
  // 四舍五入到 0 的负数（如 -0.4 → '-0'）归一为 '0'，不输出 "-0" 形态
  if (Number.parseFloat(text) === 0) {
    text = text.replace('-', '')
  }

  const negative = text.startsWith('-')
  const unsigned = negative ? text.slice(1) : text
  const body = config?.thousands === true ? groupThousands(unsigned) : unsigned
  const sign = negative ? '-' : ''

  if (format === 'currency') {
    return `${sign}${config?.currency ?? DEFAULT_CURRENCY}${body}`
  }
  if (format === 'percent') {
    return `${sign}${body}%`
  }
  return `${sign}${body}`
}
