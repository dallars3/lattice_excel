// 编辑器值解析纯逻辑（票 06）：日期三格式 + include_time + 最小 1900；数字非法/空值/超精度；
// 选项/人员匹配与 URL 自动补 https://。DOM 无关，供编辑器与剪贴板转换共用。
import { describe, expect, it } from 'vitest'

import {
  matchOptionId,
  normalizeUrlInput,
  parseDateInput,
  parseNumberDraft,
} from './editor-values'

describe('parseDateInput', () => {
  it.each([
    { text: '2024-01-05', includeTime: false },
    { text: '2024/01/05', includeTime: false },
    { text: '2024年1月5日', includeTime: false },
  ])('三种格式均解析为本地日期 $text', ({ text, includeTime }) => {
    const ms = parseDateInput(text, includeTime)
    expect(ms).not.toBeNull()
    const date = new Date(ms as number)
    expect(date.getFullYear()).toBe(2024)
    expect(date.getMonth()).toBe(0)
    expect(date.getDate()).toBe(5)
    expect(date.getHours()).toBe(0)
    expect(date.getMinutes()).toBe(0)
  })

  it('include_time 时解析时间；false 时忽略时间部分', () => {
    const withTime = parseDateInput('2024-01-05 09:30', true)
    const date = new Date(withTime as number)
    expect(date.getHours()).toBe(9)
    expect(date.getMinutes()).toBe(30)

    const withoutTime = parseDateInput('2024-01-05 09:30', false)
    const plain = new Date(withoutTime as number)
    expect(plain.getHours()).toBe(0)
    expect(plain.getMinutes()).toBe(0)
  })

  it('最小 1900-01-01：更早日期返回 null', () => {
    expect(parseDateInput('1899-12-31', false)).toBeNull()
    const min = parseDateInput('1900-01-01', false)
    expect(new Date(min as number).getFullYear()).toBe(1900)
  })

  it.each(['', 'abc', '2024-13-01', '2024-02-30', '2024-01-05 25:00', '24-1-5'])(
    '非法输入返回 null：$text',
    (text) => {
      expect(parseDateInput(text, true)).toBeNull()
    },
  )
})

describe('parseNumberDraft', () => {
  it('空串 → null（空值与 0 区分）', () => {
    expect(parseNumberDraft('', 2)).toBeNull()
    expect(parseNumberDraft('   ', 2)).toBeNull()
  })

  it('0 是合法数字（不得归一为空）', () => {
    expect(parseNumberDraft('0', 0)).toBe(0)
    expect(parseNumberDraft('-0', 2)).toBe(0)
    expect(parseNumberDraft('0.00', 2)).toBe(0)
  })

  it('超精度四舍五入到小数位', () => {
    expect(parseNumberDraft('1.23456', 2)).toBe(1.23)
    expect(parseNumberDraft('1.2356', 2)).toBe(1.24)
    expect(parseNumberDraft('1.999', 1)).toBe(2)
    expect(parseNumberDraft('12.34567', 4)).toBe(12.3457)
    expect(parseNumberDraft('12.34567', 0)).toBe(12)
  })

  it('非法数字返回 undefined（编辑器据此行内报错且不提交）', () => {
    expect(parseNumberDraft('1..2', 2)).toBeUndefined()
    expect(parseNumberDraft('abc', 2)).toBeUndefined()
    expect(parseNumberDraft('-', 2)).toBeUndefined()
    expect(parseNumberDraft('1e999', 2)).toBeUndefined()
  })
})

describe('matchOptionId', () => {
  const options = [
    { id: 'opt_1', label: '进行中' },
    { id: 'opt_2', label: '完成' },
  ]

  it('按选项名大小写不敏感匹配，也接受 option_id', () => {
    expect(matchOptionId(options, '完成')).toBe('opt_2')
    expect(matchOptionId(options, 'opt_1')).toBe('opt_1')
    expect(matchOptionId(options, ' OPT_2 ')).toBe('opt_2')
  })

  it('无匹配返回 null', () => {
    expect(matchOptionId(options, '暂停')).toBeNull()
    expect(matchOptionId(options, '')).toBeNull()
  })
})

describe('normalizeUrlInput', () => {
  it('无协议自动补 https://', () => {
    expect(normalizeUrlInput('example.com', '')).toEqual({
      url: 'https://example.com',
      displayText: 'https://example.com',
    })
  })

  it('保留显示文本', () => {
    expect(normalizeUrlInput('https://example.com', '示例')).toEqual({
      url: 'https://example.com',
      displayText: '示例',
    })
  })

  it('仅允许 http/https；其他协议/非法地址返回 null', () => {
    expect(normalizeUrlInput('javascript:alert(1)', '')).toBeNull()
    expect(normalizeUrlInput('ftp://example.com', '')).toBeNull()
    expect(normalizeUrlInput('', '')).toBeNull()
  })
})
