// TSV 剪贴板（票 06）：选区序列化 + 单值/单行粘贴计划（纯逻辑）。
// 值格式按字段类型（需求 4.4 / AC-REC-04）：文本原文、数字原值、选项名、日期按格式、
// 人员显示名、附件 id、公式结果、关联标签、引用值；只读列跳过、选项不匹配置空分别计数。
import { formatDate } from '@/core/grid/renderers/format-date'
import { matchOptionId, normalizeUrlInput, parseDateInput } from '@/core/grid/renderers/editor-values'
import { resolveSelectOptions } from '@/core/grid/renderers/select'
import { parseFormulaValue } from '@/core/grid/renderers/formula'
import { parseUrlValue } from '@/core/grid/renderers/url'
import type { SelectionRect } from '@/core/grid'

/** 剪贴板字段投影（避免把 workspace 字段类型绑进纯逻辑）。 */
export interface ClipboardField {
  readonly id: string
  readonly name: string
  readonly type: string
  readonly configMap: Readonly<Record<string, unknown>>
}

export interface ClipboardRow {
  readonly cellsMap: Readonly<Record<string, unknown>>
}

export interface ClipboardExtras {
  readonly personDirectory?: Readonly<Record<string, { readonly name: string }>>
  readonly relationLabels?: Readonly<Record<string, string>>
}

function asList(value: unknown): string[] {
  if (Array.isArray(value)) return value.map((item) => String(item))
  if (value === null || value === undefined || value === '') return []
  return [String(value)]
}

/** 单元格值 → 剪贴板文本（TSV 字段内不保留换行/制表符，替换为空格）。 */
export function serializeCellValue(
  field: ClipboardField,
  value: unknown,
  extras: ClipboardExtras = {},
): string {
  if (value === null || value === undefined) return ''
  let text: string
  switch (field.type) {
    case 'number':
      text = typeof value === 'number' && Number.isFinite(value) ? String(value) : String(value)
      break
    case 'select': {
      const options = resolveSelectOptions(field.configMap as never)
      text = options.find((option) => option.id === value)?.label ?? String(value)
      break
    }
    case 'multiselect': {
      const options = resolveSelectOptions(field.configMap as never)
      text = asList(value)
        .map((id) => options.find((option) => option.id === id)?.label ?? id)
        .join(',')
      break
    }
    case 'date':
      text = formatDate(value, field.configMap as never)
      break
    case 'person': {
      const directory = extras.personDirectory ?? {}
      text = asList(value)
        .map((userId) => directory[userId]?.name ?? userId)
        .join(',')
      break
    }
    case 'attachment':
      text = asList(value).join(',')
      break
    case 'url': {
      const parsed = parseUrlValue(value)
      text = parsed?.displayText ?? (typeof value === 'string' ? value : '')
      break
    }
    case 'formula': {
      const parsed = parseFormulaValue(value)
      text = parsed.text
      break
    }
    case 'relation': {
      const labels = extras.relationLabels ?? {}
      text = asList(value)
        .map((recordId) => labels[recordId] ?? recordId)
        .join(',')
      break
    }
    default:
      text = String(value)
      break
  }
  return text.replace(/\t/g, ' ').replace(/\r?\n/g, ' ')
}

/** 选区 → TSV（行内单元格之间为制表符，行之间为换行）。 */
export function serializeSelection(options: {
  readonly rows: readonly ClipboardRow[]
  readonly fields: readonly ClipboardField[]
  readonly rect: SelectionRect
  readonly extras?: ClipboardExtras
}): string {
  const { rows, fields, rect, extras } = options
  const lines: string[] = []
  for (let row = rect.top; row <= rect.bottom; row += 1) {
    const record = rows[row]
    if (record === undefined) continue
    const cells: string[] = []
    for (let col = rect.left; col <= rect.right; col += 1) {
      const field = fields[col]
      if (field === undefined) {
        cells.push('')
        continue
      }
      cells.push(serializeCellValue(field, record.cellsMap[field.id], extras))
    }
    lines.push(cells.join('\t'))
  }
  return lines.join('\n')
}

/** 解析 TSV 文本：去尾部空行；行内按制表符拆分。 */
export function parseTsv(text: string): string[][] {
  const normalized = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
  const lines = normalized.split('\n')
  while (lines.length > 0 && lines[lines.length - 1] === '') lines.pop()
  return lines.map((line) => line.split('\t'))
}

export interface PasteOperation {
  readonly fieldId: string
  readonly value: unknown
  readonly expectVersion: number
}

export interface PastePlan {
  readonly operations: readonly PasteOperation[];
  /** 只读列（公式/引用/附件）跳过格数。 */
  readonly skippedReadonly: number;
  /** 选项不匹配置空计数。 */
  readonly optionUnmatched: number;
  /** 日期/数字/URL/人员/关联无法换算计数（按空值写入）。 */
  readonly invalidCount: number;
  /** 二维区域（多行）粘贴：按 G-5 不支持，operations 为空。 */
  readonly unsupported: boolean;
}

const READONLY_TYPES = new Set(['formula', 'lookup', 'attachment'])

function splitMulti(text: string): string[] {
  return text
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter((item) => item !== '')
}

interface CellConversion {
  readonly value: unknown;
  readonly unmatched?: boolean;
  readonly invalid?: boolean;
}

function convertCell(
  field: ClipboardField,
  text: string,
  extras: ClipboardExtras,
): CellConversion {
  if (text === '') return { value: null }
  switch (field.type) {
    case 'text':
      return { value: text }
    case 'number': {
      const parsed = Number(text)
      return Number.isFinite(parsed) ? { value: parsed } : { value: null, invalid: true }
    }
    case 'select': {
      const options = resolveSelectOptions(field.configMap as never)
      const matched = matchOptionId(options, text)
      return matched === null ? { value: null, unmatched: true } : { value: matched }
    }
    case 'multiselect': {
      const options = resolveSelectOptions(field.configMap as never)
      const tokens = splitMulti(text)
      let unmatched = false
      const ids: string[] = []
      for (const token of tokens) {
        const matched = matchOptionId(options, token)
        if (matched === null) unmatched = true
        else ids.push(matched)
      }
      return { value: ids.length === 0 ? null : ids, unmatched }
    }
    case 'date': {
      const includeTime = field.configMap.include_time === true
      const parsed = parseDateInput(text, includeTime)
      return parsed === null ? { value: null, invalid: true } : { value: parsed }
    }
    case 'person': {
      const directory = extras.personDirectory ?? {}
      const allowMultiple = field.configMap.allow_multiple === true
      const tokens = splitMulti(text)
      const ids: string[] = []
      let unmatched = false
      for (const token of tokens) {
        const byName = Object.entries(directory).find(([, info]) => info.name === token)
        const id = byName?.[0] ?? (directory[token] !== undefined ? token : token)
        if (byName === undefined && directory[token] === undefined) unmatched = true
        ids.push(id)
      }
      const value = allowMultiple ? (ids.length === 0 ? null : ids) : (ids[0] ?? null)
      return { value, unmatched }
    }
    case 'url': {
      const normalized = normalizeUrlInput(text, '')
      return normalized === null
        ? { value: null, invalid: true }
        : { value: { url: normalized.url, display_text: normalized.displayText } }
    }
    case 'relation': {
      const labels = extras.relationLabels ?? {}
      const allowMultiple = field.configMap.allow_multiple === true
      const tokens = splitMulti(text)
      const ids: string[] = []
      let unmatched = false
      for (const token of tokens) {
        const byLabel = Object.entries(labels).find(([, label]) => label === token)
        const id = byLabel?.[0] ?? (labels[token] !== undefined ? token : token)
        if (byLabel === undefined && labels[token] === undefined) unmatched = true
        ids.push(id)
      }
      const value = allowMultiple ? (ids.length === 0 ? null : ids) : (ids[0] ?? null)
      return { value, unmatched }
    }
    default:
      return { value: null }
  }
}

/**
 * 粘贴计划：仅支持单值或单行；从 startCol 起按字段顺序映射。
 * 只读列跳过、选项不匹配/其它无法换算按空值写入并分别计数。
 */
export function buildPastePlan(options: {
  readonly parsed: readonly (readonly string[])[]
  readonly fields: readonly ClipboardField[]
  readonly startCol: number
  readonly extras?: ClipboardExtras
}): PastePlan {
  const { parsed, fields, startCol, extras = {} } = options
  if (parsed.length > 1) {
    return {
      operations: [],
      skippedReadonly: 0,
      optionUnmatched: 0,
      invalidCount: 0,
      unsupported: true,
    }
  }
  const row = parsed[0] ?? []
  const operations: PasteOperation[] = []
  let skippedReadonly = 0
  let optionUnmatched = 0
  let invalidCount = 0

  row.forEach((text, index) => {
    const field = fields[startCol + index]
    if (field === undefined) return
    if (READONLY_TYPES.has(field.type)) {
      skippedReadonly += 1
      return
    }
    const conversion = convertCell(field, text.trim(), extras)
    if (conversion.unmatched === true) optionUnmatched += 1
    if (conversion.invalid === true) invalidCount += 1
    operations.push({ fieldId: field.id, value: conversion.value, expectVersion: 0 })
  })

  return { operations, skippedReadonly, optionUnmatched, invalidCount, unsupported: false }
}

/** 粘贴结果提示（合并服务端 skipped_count / fail_row_list 与本地跳过计数）。 */
export function buildPasteSummary(plan: PastePlan, serverSkipped = 0, failedRows = 0): string {
  if (plan.unsupported) return '当前仅支持单值或单行粘贴'
  const parts = [`已粘贴 ${plan.operations.length - failedRows} 个单元格`]
  if (plan.skippedReadonly > 0) parts.push(`跳过只读列 ${plan.skippedReadonly} 个`)
  if (plan.optionUnmatched > 0) parts.push(`选项不匹配置空 ${plan.optionUnmatched} 个`)
  if (plan.invalidCount > 0) parts.push(`无法识别置空 ${plan.invalidCount} 个`)
  if (serverSkipped > 0) parts.push(`服务端跳过 ${serverSkipped} 项`)
  if (failedRows > 0) parts.push(`${failedRows} 项写入失败`)
  return parts.join('；')
}

/** 剪贴板适配器（测试可注入；缺省走 navigator.clipboard + execCommand 兜底）。 */
export interface ClipboardAdapter {
  readonly writeText: (text: string) => Promise<void>
  readonly readText: () => Promise<string>
}

export function defaultClipboard(): ClipboardAdapter {
  return {
    async writeText(text: string): Promise<void> {
      if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText !== undefined) {
        await navigator.clipboard.writeText(text)
        return
      }
      if (typeof document !== 'undefined') {
        const area = document.createElement('textarea')
        area.value = text
        area.style.position = 'fixed'
        area.style.opacity = '0'
        document.body.appendChild(area)
        area.select()
        document.execCommand?.('copy')
        document.body.removeChild(area)
      }
    },
    async readText(): Promise<string> {
      if (typeof navigator !== 'undefined' && navigator.clipboard?.readText !== undefined) {
        return navigator.clipboard.readText()
      }
      return ''
    },
  }
}
