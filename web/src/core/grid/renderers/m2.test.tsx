// M2 11 类渲染器行为测试（票 05）：数字/日期格式已单独纯函数测试，本文件覆盖
// 多选/日期/人员/附件/超链接/公式/关联/引用/单选颜色映射的渲染取值与边界。
import '@testing-library/jest-dom/vitest'

import { createElement } from 'react'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { CellRendererDef, CellRendererProps } from '../registry'
import { ATTACHMENT_RENDERER } from './attachment'
import { DATE_RENDERER } from './date'
import { FORMULA_RENDERER, parseFormulaValue } from './formula'
import { LOOKUP_RENDERER } from './lookup'
import { MULTISELECT_RENDERER } from './multiselect'
import { PERSON_RENDERER } from './person'
import { RELATION_RENDERER } from './relation'
import { SELECT_RENDERER, optionColorValue } from './select'
import { URL_RENDERER, parseUrlValue } from './url'

function renderCell(def: CellRendererDef, value: unknown, config: unknown): ReturnType<typeof render> {
  const props: CellRendererProps = { value, config }
  return render(createElement(def.component, props))
}

describe('select / multiselect（M2 选项颜色与多标签）', () => {
  it('后端 option_list 颜色名映射为调色板色值，未知色安全回退中性样式', () => {
    expect(optionColorValue('blue')).toBe('#3370ff')
    expect(optionColorValue('unknown')).toBeUndefined()
    expect(optionColorValue(undefined)).toBeUndefined()
    expect(optionColorValue('#123456')).toBe('#123456')
  })

  it('select 兼容后端 option_list {id,name,color} 并渲染带色标签', () => {
    renderCell(SELECT_RENDERER, 'opt_1', {
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
    })

    const tag = screen.getByText('进行中')
    expect(tag.style.backgroundColor).toBe('rgb(51, 112, 255)')
  })

  it('multiselect 渲染多个标签，未知 id 安全显示原文', () => {
    renderCell(MULTISELECT_RENDERER, ['opt_1', 'opt_2', 'opt_ghost'], {
      option_list: [
        { id: 'opt_1', name: '待办', color: 'orange' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
    })

    expect(screen.getByText('待办')).toBeInTheDocument()
    expect(screen.getByText('完成')).toBeInTheDocument()
    expect(screen.getByText('opt_ghost')).toBeInTheDocument()
  })

  it('multiselect 空数组渲染空内容', () => {
    const { container } = renderCell(MULTISELECT_RENDERER, [], { option_list: [] })
    expect(container.textContent).toBe('')
  })
})

describe('date（M2 三种格式 + include_time）', () => {
  it.each([
    { config: { format: 'YYYY-MM-DD', include_time: false }, expected: '2024-01-05' },
    { config: { format: 'YYYY/MM/DD', include_time: false }, expected: '2024/01/05' },
    { config: { format: 'YYYY年M月D日', include_time: false }, expected: '2024年1月5日' },
    { config: { format: 'YYYY-MM-DD', include_time: true }, expected: '2024-01-05 09:08' },
  ])('config=$config → "$expected"', ({ config, expected }) => {
    const value = new Date(2024, 0, 5, 9, 8).getTime()
    renderCell(DATE_RENDERER, value, config)
    expect(screen.getByText(expected)).toBeInTheDocument()
  })

  it('null 渲染空内容', () => {
    const { container } = renderCell(DATE_RENDERER, null, {})
    expect(container.textContent).toBe('')
  })
})

describe('person（M2 头像 + 显示名 + 停用/未知灰显）', () => {
  const USERS = {
    usr_1: { name: '张三' },
    usr_2: { name: '李四', disabled: true },
  }

  it('多人并列渲染显示名，首字头像块', () => {
    const { container } = renderCell(PERSON_RENDERER, ['usr_1', 'usr_2'], { users: USERS })

    expect(screen.getByText('张三')).toBeInTheDocument()
    expect(screen.getByText('李四')).toBeInTheDocument()
    const avatars = within(container).getAllByText(/张|李/).filter((node) => node.className.includes('avatar'))
    expect(avatars).toHaveLength(2)
  })

  it('停用成员灰显并提示，未知成员回退 user_id', () => {
    const { container } = renderCell(PERSON_RENDERER, ['usr_2', 'usr_missing'], { users: USERS })

    expect(screen.getByText('李四').closest('[data-disabled="true"]')).not.toBeNull()
    expect(screen.getByText('usr_missing')).toBeInTheDocument()
    expect(container.querySelector('[data-unknown="true"]')).not.toBeNull()
  })
})

describe('attachment（M2 缩略图 + 文件名）', () => {
  it('file_id 列表渲染缩略图与文件名映射，缺 metadata 用 id 兜底', () => {
    renderCell(ATTACHMENT_RENDERER, ['fil_1', 'fil_2'], {
      files: { fil_1: { name: '合同.pdf' } },
    })

    expect(screen.getByText('合同.pdf')).toBeInTheDocument()
    expect(screen.getByText('fil_2')).toBeInTheDocument()
    const thumb = screen.getByRole('img', { name: '合同.pdf' })
    expect(thumb.getAttribute('src')).toBe('/api/v1/files/fil_1/thumb')
  })
})

describe('url（M2 协议校验 + 新窗口安全属性）', () => {
  it('解析对象与 JSON 字符串，仅接受 http/https', () => {
    expect(parseUrlValue({ url: 'https://a.com', display_text: '官网' })).toEqual({
      url: 'https://a.com',
      displayText: '官网',
    })
    expect(parseUrlValue('{"url":"https://b.com","display_text":"文档"}')).toEqual({
      url: 'https://b.com',
      displayText: '文档',
    })
    expect(parseUrlValue('https://c.com')).toEqual({ url: 'https://c.com', displayText: 'https://c.com' })
    expect(parseUrlValue({ url: 'javascript:alert(1)', display_text: '坏链接' })).toBeNull()
    expect(parseUrlValue('not-a-url')).toBeNull()
    expect(parseUrlValue(null)).toBeNull()
  })

  it('渲染显示文本链接并在新窗口打开，rel 防 opener', () => {
    renderCell(URL_RENDERER, { url: 'https://a.com', display_text: '官网' }, {})

    const link = screen.getByRole('link', { name: '官网' })
    expect(link).toHaveAttribute('href', 'https://a.com')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('非法协议不渲染链接，退化为纯文本', () => {
    renderCell(URL_RENDERER, { url: 'javascript:alert(1)', display_text: '坏链接' }, {})

    expect(screen.getByText('坏链接')).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})

describe('formula（M2 结果 / 计算中占位 / #ERROR 哨兵）', () => {
  it.each([
    { value: null, text: '计算中…', reason: null, error: false },
    { value: 42, text: '42', reason: null, error: false },
    { value: '42', text: '42', reason: null, error: false },
    { value: '#ERROR:除数为空', text: '#ERROR', reason: '除数为空', error: true },
    { value: '#ERROR', text: '#ERROR', reason: '#ERROR', error: true },
  ])('parseFormulaValue($value) → $text', ({ value, text, reason, error }) => {
    expect(parseFormulaValue(value)).toEqual({ text, reason, error })
  })

  it('错误值渲染 #ERROR 且 title 显示原因；null 显示占位而非 0', () => {
    renderCell(FORMULA_RENDERER, '#ERROR:除数为空', {})

    const node = screen.getByText('#ERROR')
    expect(node).toHaveAttribute('title', '除数为空')
  })

  it('null 渲染计算中占位', () => {
    renderCell(FORMULA_RENDERER, null, {})
    expect(screen.getByText('计算中…')).toBeInTheDocument()
  })
})

describe('relation（M2 主字段标签 + 可跳转回调）', () => {
  it('多条并列渲染目标主字段值，点击调用 onOpenRecord', () => {
    const onOpenRecord = vi.fn()
    render(
      createElement(RELATION_RENDERER.component as never, {
        value: ['rec_1', 'rec_2'],
        config: { labels: { rec_1: '客户甲', rec_2: '客户乙' } },
        onOpenRecord,
      } as never),
    )

    expect(screen.getByText('客户甲')).toBeInTheDocument()
    expect(screen.getByText('客户乙')).toBeInTheDocument()
    fireEvent.click(screen.getByText('客户甲'))
    expect(onOpenRecord).toHaveBeenCalledWith('rec_1')
  })

  it('未知记录 id 回退显示 id', () => {
    renderCell(RELATION_RENDERER, 'rec_missing', { labels: {} })
    expect(screen.getByText('rec_missing')).toBeInTheDocument()
  })
})

describe('lookup（M2 按被引用字段类型复用渲染器、多值并列）', () => {
  it('数字类型按数字格式渲染', () => {
    renderCell(LOOKUP_RENDERER, [126, 99], {
      targetType: 'number',
      targetConfig: { format: 'decimal', decimals: 2, thousands: true },
    })

    expect(screen.getByText('126.00')).toBeInTheDocument()
    expect(screen.getByText('99.00')).toBeInTheDocument()
  })

  it('文本类型按文本渲染（多值并列）', () => {
    renderCell(LOOKUP_RENDERER, ['甲', '乙'], { targetType: 'text', targetConfig: {} })

    expect(screen.getByText('甲')).toBeInTheDocument()
    expect(screen.getByText('乙')).toBeInTheDocument()
  })

  it('单选类型复用选项标签渲染', () => {
    renderCell(LOOKUP_RENDERER, 'opt_1', {
      targetType: 'select',
      targetConfig: { option_list: [{ id: 'opt_1', name: '进行中', color: 'blue' }] },
    })

    expect(screen.getByText('进行中')).toBeInTheDocument()
  })

  it('空值渲染空内容', () => {
    const { container } = renderCell(LOOKUP_RENDERER, null, { targetType: 'text', targetConfig: {} })
    expect(container.textContent).toBe('')
  })
})
