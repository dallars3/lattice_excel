// formatNumber 纯函数（票 08 number 演示；需求 4.3 数字字段行的展示配置子集）。
// M1 演示范围（票面 AC）：整数 / 小数位数（0-4）/ 千分位开关 / 超精度四舍五入 / 空值与 0 区分；
// 百分比与货币格式（需求 4.3 数字行的其余配置）属 M2 完整实现，此处不前置。
/** number 字段类型配置（M1 演示子集；M2 扩展百分比/货币时在此追加，不破坏已有键）。 */
export interface NumberCellConfig {
  /** 小数位数：0-4（需求 4.3「小数 0-4 位」），超出钳制；超精度四舍五入。缺省 0 = 整数。 */
  readonly precision?: number
  /** 千分位分隔开关。缺省关。 */
  readonly thousands?: boolean
}

const MAX_PRECISION = 4
const THOUSANDS_GROUP = /\B(?=(\d{3})+(?!\d))/g

/** 数字 → 展示文本：null/undefined/非有限值 → 空串（空值与 0 区分显示，需求 4.3）；0 → '0'。 */
export function formatNumber(value: number | null | undefined, config?: NumberCellConfig): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return ''
  }
  const precision = Math.min(Math.max(0, Math.trunc(config?.precision ?? 0)), MAX_PRECISION)
  let text = value.toFixed(precision)
  // 四舍五入到 0 的负数（如 -0.4 → '-0'）归一为 '0'，不输出 "-0" 形态
  if (Number.parseFloat(text) === 0) {
    text = text.replace('-', '')
  }
  if (config?.thousands !== true) {
    return text
  }
  const [intPart, decimalPart] = text.split('.')
  const negative = intPart.startsWith('-')
  const grouped = (negative ? intPart.slice(1) : intPart).replace(THOUSANDS_GROUP, ',')
  return `${negative ? '-' : ''}${grouped}${decimalPart === undefined ? '' : `.${decimalPart}`}`
}
