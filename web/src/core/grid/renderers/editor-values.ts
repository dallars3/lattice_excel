// 编辑器值解析纯逻辑（票 06）：与 DOM 无关，供编辑器组件与剪贴板转换共用。
// 日期三种格式 + include_time + 最小 1900-01-01（需求 4.3 日期行）；数字非法字符不可录、
// 超精度四舍五入、空值与 0 区分；URL 仅 http/https 并自动补 https://。

/** 最小日期 1900-01-01（本地时区）。 */
export const MIN_DATE_MS = new Date(1900, 0, 1, 0, 0, 0, 0).getTime()

const DATE_PATTERN_SLASH_DASH =
 /^\s*(\d{4})[\/-](\d{1,2})[\/-](\d{1,2})(?:[ T](\d{1,2}):(\d{2}))?\s*$/
const DATE_PATTERN_CN =
 /^\s*(\d{4})年(\d{1,2})月(\d{1,2})日(?:[ T](\d{1,2}):(\d{2}))?\s*$/

function daysInMonth(year: number, month: number): number {
  return new Date(year, month, 0).getDate()
}

/**
 * 解析日期文本为毫秒时间戳；支持 YYYY/MM/DD、YYYY-MM-DD、YYYY年M月D日（可带 HH:mm）。
 * includeTime=false 时忽略时间部分（置 00:00）；非法/早于 1900-01-01 返回 null。
 */
export function parseDateInput(text: string, includeTime: boolean): number | null {
  const match = DATE_PATTERN_SLASH_DASH.exec(text) ?? DATE_PATTERN_CN.exec(text)
  if (match === null) return null
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const hour = includeTime && match[4] !== undefined ? Number(match[4]) : 0
  const minute = includeTime && match[5] !== undefined ? Number(match[5]) : 0
  if (month < 1 || month > 12) return null
  if (day < 1 || day > daysInMonth(year, month)) return null
  if (hour > 23 || minute > 59) return null
  const timestamp = new Date(year, month - 1, day, hour, minute, 0, 0).getTime()
  if (!Number.isFinite(timestamp) || timestamp < MIN_DATE_MS) return null
  return timestamp
}

/** 小数位钳制到 0-4（接口文档 8.0.4）。 */
function clampDecimals(decimals: number): number {
  const value = Math.trunc(decimals)
  if (!Number.isFinite(value)) return 0
  return Math.min(Math.max(0, value), 4)
}

/**
 * 数字草稿 → 提交值：空串 → null；非法 → undefined；超精度四舍五入。
 * 0 是合法值（与空值严格区分）。
 */
export function parseNumberDraft(text: string, decimals: number): number | null | undefined {
  const trimmed = text.trim()
  if (trimmed === '') return null
  if (!/^-?\d*\.?\d+$/.test(trimmed)) return undefined
  const value = Number(trimmed)
  if (!Number.isFinite(value)) return undefined
  const precision = clampDecimals(decimals)
  // toFixed 实现四舍五入（二进制末尾误差在 0-4 位精度内可忽略）
  return Number(value.toFixed(precision))
}

export interface MatchableOption {
  readonly id: string
  readonly label: string
}

/** 选项文本/ID → option_id；大小写不敏感、去空格；无匹配 null。 */
export function matchOptionId(
  options: readonly MatchableOption[],
  text: string,
): string | null {
  const target = text.trim().toLocaleLowerCase()
  if (target === '') return null
  const byLabel = options.find((option) => option.label.trim().toLocaleLowerCase() === target)
  if (byLabel !== undefined) return byLabel.id
  const byId = options.find((option) => option.id.toLocaleLowerCase() === target)
  return byId?.id ?? null
}

/** URL + 显示文本 → {url, display_text}；无协议自动补 https://，仅 http/https，非法 null。 */
export function normalizeUrlInput(
  rawUrl: string,
  rawDisplay: string,
): { url: string; displayText: string } | null {
  const input = rawUrl.trim()
  if (input === '') return null
  const withProtocol = /^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(input) ? input : `https://${input}`
  let parsed: URL
  try {
    parsed = new URL(withProtocol)
  } catch {
    return null
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null
  const display = rawDisplay.trim()
  return { url: withProtocol, displayText: display === '' ? withProtocol : display }
}
