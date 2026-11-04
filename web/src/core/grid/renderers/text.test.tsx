// text 演示（票 08）测试：渲染器（长文本单行省略、全量值 title 通道、空值安全）+
// 编辑器（统一生命周期 begin/input/validate/commit/reject，Enter/Tab/Esc 三键一致，RTL）。
import '@testing-library/jest-dom/vitest'

import { createElement } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { EditorCommitMove } from '../registry'
import { TEXT_EDITOR, TEXT_LIFECYCLE, TEXT_RENDERER } from './text'
import styles from './text.module.css'

const LONG_TEXT =
  '这是一段远超单元格宽度的长文本，用于验证单行省略语义：多行文本字段在网格中单行显示、' +
  '溢出省略，全量值经 title/aria 或可取值接口获取（需求 4.3 多行文本行）。'.repeat(3)

function renderCell(value: unknown, config: unknown = undefined): ReturnType<typeof render> {
  return render(createElement(TEXT_RENDERER.component, { value, config }))
}

describe('TEXT_RENDERER（票 08 text 演示渲染器）', () => {
  it('定义语义元数据：左对齐 + 单行省略裁剪（技术方案 6.4 渲染器语义）', () => {
    expect(TEXT_RENDERER.align).toBe('left')
    expect(TEXT_RENDERER.clip).toBe('ellipsis')
  })

  it('长文本渲染：可见文本为全量值，单行省略样式模块挂载', () => {
    const { container } = renderCell(LONG_TEXT)
    const cell = container.querySelector('span')

    expect(screen.getByText(LONG_TEXT)).toBeInTheDocument()
    expect(cell).toHaveClass(styles.text)
  })

  it('全量值可获取：title 属性承载全量文本（hover/辅助技术通道）', () => {
    const { container } = renderCell(LONG_TEXT)

    expect(container.querySelector('span')).toHaveAttribute('title', LONG_TEXT)
  })

  it.each([
    { value: null, expected: '' },
    { value: undefined, expected: '' },
    { value: 42, expected: '42' },
    { value: '短文本', expected: '短文本' },
  ])('安全显示：value=$value → "$expected"，不崩溃', ({ value, expected }) => {
    const { container } = renderCell(value)

    expect(container.querySelector('span')?.textContent).toBe(expected)
  })
})

describe('TEXT_LIFECYCLE（票 08 编辑器统一生命周期 · text 演示钩子）', () => {
  it.each([
    { value: 'abc', expected: 'abc' },
    { value: null, expected: '' },
    { value: 42, expected: '42' },
  ])('begin：当前单元格值 → 初始草稿（$value → "$expected"）', ({ value, expected }) => {
    expect(TEXT_LIFECYCLE.begin({ value, config: undefined })).toBe(expected)
  })

  it('input：一次输入 → 更新后的草稿（受控）', () => {
    expect(TEXT_LIFECYCLE.input({ draft: 'a', input: 'ab', config: undefined })).toBe('ab')
  })

  it.each([
    { draft: 'abc', config: undefined, expected: null }, // 缺省上限 10,000（需求 4.3 多行文本 ≤10,000 字符）
    { draft: 'x'.repeat(10000), config: undefined, expected: null },
    { draft: 'x'.repeat(10001), config: undefined, expected: '内容超出上限 10000 字符' },
    { draft: 'abcd', config: { maxLength: 3 }, expected: '内容超出上限 3 字符' },
    { draft: 'abc', config: { maxLength: 3 }, expected: null },
  ])('validate：超上限返回可访问错误文案，通过返回 null', ({ draft, config, expected }) => {
    expect(TEXT_LIFECYCLE.validate({ draft, config })).toBe(expected)
  })

  it('commit：草稿 → 提交值', () => {
    expect(TEXT_LIFECYCLE.commit({ draft: '草稿', config: undefined })).toBe('草稿')
  })
})

describe('TEXT_EDITOR（票 08 text 演示编辑器 · 三键一致 RTL）', () => {
  function renderEditor(overrides: { value?: unknown; config?: unknown; enterMove?: EditorCommitMove } = {}) {
    const onCommit = vi.fn()
    const onReject = vi.fn()
    render(
      createElement(TEXT_EDITOR.component, {
        value: '初始值',
        config: undefined,
        onCommit,
        onReject,
        ...overrides,
      }),
    )
    return { onCommit, onReject, input: screen.getByRole('textbox') }
  }

  it('begin：以当前单元格值初始化草稿', () => {
    const { input } = renderEditor()

    expect(input).toHaveValue('初始值')
  })

  it('begin：空单元格以空草稿进入编辑', () => {
    const { input } = renderEditor({ value: null })

    expect(input).toHaveValue('')
  })

  it('input：输入更新草稿（受控）', () => {
    const { input } = renderEditor()

    fireEvent.change(input, { target: { value: '更新值' } })
    expect(input).toHaveValue('更新值')
  })

  it.each([
    { key: 'Enter', shiftKey: false, move: 'down' }, // Enter 提交并下移（AC-REC-02，默认语义）
    { key: 'Tab', shiftKey: false, move: 'right' }, // Tab 提交并右移
    { key: 'Tab', shiftKey: true, move: 'left' }, // Shift+Tab 提交并左移
  ])('三键提交一致：$key（shift=$shiftKey）→ move=$move', ({ key, shiftKey, move }) => {
    const { onCommit, onReject, input } = renderEditor()
    fireEvent.change(input, { target: { value: '提交值' } })
    fireEvent.keyDown(input, { key, shiftKey })

    expect(onCommit).toHaveBeenCalledWith('提交值', move)
    expect(onReject).not.toHaveBeenCalled()
  })

  it('Enter 移动语义可配：enterMove="none"（对接票 07 键位信号的配置缝）', () => {
    const { onCommit, input } = renderEditor({ enterMove: 'none' })

    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onCommit).toHaveBeenCalledWith('初始值', 'none')
  })

  it('Esc 取消：走 reject 分支（丢弃草稿、不提交）', () => {
    const { onCommit, onReject, input } = renderEditor()

    fireEvent.keyDown(input, { key: 'Escape' })

    expect(onReject).toHaveBeenCalledTimes(1)
    expect(onCommit).not.toHaveBeenCalled()
  })

  it('validate 失败阻止提交：行内错误可访问（role=alert + aria-invalid），内容不丢失', () => {
    const { onCommit, input } = renderEditor({ config: { maxLength: 5 } })

    fireEvent.change(input, { target: { value: '超长内容六六六' } }) // 7 字 > 5
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('内容超出上限 5 字符')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveValue('超长内容六六六') // 校验异常内容不丢失（需求 4.4）
  })

  it('修正后错误即时消除，可正常提交', () => {
    const { onCommit, input } = renderEditor({ config: { maxLength: 5 } })

    fireEvent.change(input, { target: { value: '超长内容六六六' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    fireEvent.change(input, { target: { value: '短' } })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith('短', 'down')
  })
})
