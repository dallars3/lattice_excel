// url 渲染器（票 05）：需求 4.3 超链接行「显示文本，新窗口打开（编辑态不跳转）」。
// 值支持 {url, display_text} 对象或 JSON 字符串；仅 http/https 可渲染为链接，
// 非法协议/非法地址退化为纯文本（绝不把 javascript: 等协议注入 href）。
import { defineRenderer } from '../registry'

import styles from './m2.module.css'

export interface UrlCellValue {
  readonly url: string
  readonly displayText: string
}

function parseObject(value: unknown): UrlCellValue | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
  const record = value as { url?: unknown; display_text?: unknown }
  if (typeof record.url !== 'string') return null
  return validate(record.url, typeof record.display_text === 'string' ? record.display_text : '')
}

function validate(rawUrl: string, rawDisplay: string): UrlCellValue | null {
  const url = rawUrl.trim()
  if (url === '') return null
  try {
    const parsed = new URL(url)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null
  } catch {
    return null
  }
  const displayText = rawDisplay.trim() === '' ? url : rawDisplay
  return { url, displayText }
}

/** 解析 URL 单元格值；非法协议/非法地址返回 null（调用方退化为纯文本）。 */
export function parseUrlValue(value: unknown): UrlCellValue | null {
  if (value === null || value === undefined) return null
  if (typeof value === 'string') {
    const text = value.trim()
    if (text === '') return null
    if (text.startsWith('{')) {
      try {
        const parsed = JSON.parse(text) as unknown
        return parseObject(parsed)
      } catch {
        return null
      }
    }
    return validate(text, '')
  }
  return parseObject(value)
}

function fallbackText(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') {
    if (value.trim().startsWith('{')) {
      try {
        const parsed = JSON.parse(value) as { display_text?: unknown; url?: unknown }
        if (typeof parsed.display_text === 'string' && parsed.display_text !== '') {
          return parsed.display_text
        }
        if (typeof parsed.url === 'string') return parsed.url
      } catch {
        // 非法 JSON：按原文展示
      }
    }
    return value
  }
  if (typeof value === 'object') {
    const record = value as { display_text?: unknown; url?: unknown }
    if (typeof record.display_text === 'string') return record.display_text
    if (typeof record.url === 'string') return record.url
  }
  return String(value)
}

function UrlCell({ value }: { readonly value: unknown; readonly config: unknown }) {
  const parsed = parseUrlValue(value)
  if (parsed === null) {
    const text = fallbackText(value)
    return (
      <span className={styles.url} title={text === '' ? undefined : text}>
        {text}
      </span>
    )
  }
  return (
    <span className={styles.url}>
      <a
        className={styles.url__link}
        href={parsed.url}
        target="_blank"
        rel="noopener noreferrer"
        title={parsed.url}
      >
        {parsed.displayText}
      </a>
    </span>
  )
}

/** 超链接字段渲染器：显示文本 + 新窗口安全打开。 */
export const URL_RENDERER = defineRenderer<unknown, unknown>({
  align: 'left',
  clip: 'ellipsis',
  component: UrlCell,
})
