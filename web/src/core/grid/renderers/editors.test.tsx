// 8 类单元格编辑器（票 06）行为测试：注册表、三键语义、行内校验、各类型专有控件。
// 只在编辑器组件公开接缝上断言（输入 → 提交值/错误），不触达内部状态命名。
import '@testing-library/jest-dom/vitest'

import { createElement } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { createCellRegistry, type CellEditorDef } from '../registry'
import {
  DATE_EDITOR,
  M2_EDITORS,
  MULTISELECT_EDITOR,
  NUMBER_EDITOR,
  PERSON_EDITOR,
  RELATION_EDITOR,
  SELECT_EDITOR,
  TEXT_EDITOR_M2,
  URL_EDITOR,
  registerM2Editors,
} from './editors'

interface RenderEditorOptions {
  value?: unknown
  config?: unknown
  initialInput?: string
}

function renderEditor(def: CellEditorDef, options: RenderEditorOptions = {}) {
  const onCommit = vi.fn()
  const onReject = vi.fn()
  render(
    createElement(def.component, {
      value: options.value ?? null,
      config: options.config,
      initialInput: options.initialInput,
      onCommit,
      onReject,
    }),
  )
  return { onCommit, onReject }
}

describe('registerM2Editors', () => {
  it('注册 8 类编辑器；formula/lookup/attachment 不提供编辑器（只读回退兜底）', () => {
    const registry = createCellRegistry()
    registerM2Editors(registry)

    for (const fieldType of ['text', 'number', 'select', 'multiselect', 'date', 'person', 'url', 'relation']) {
      expect(registry.queryEditor(fieldType)).toBe(M2_EDITORS[fieldType])
    }
    expect(Object.keys(M2_EDITORS).sort()).toEqual([
      'date',
      'multiselect',
      'number',
      'person',
      'relation',
      'select',
      'text',
      'url',
    ])
  })

  it('text 直接复用 M1 统一生命周期编辑器', () => {
    expect(TEXT_EDITOR_M2).toBe(M2_EDITORS.text)
  })
})

describe('number 编辑器', () => {
  it('非法字符不可录；超精度四舍五入后提交', () => {
    const { onCommit } = renderEditor(NUMBER_EDITOR, {
      value: 1,
      config: { format: 'decimal', decimals: 2 },
    })
    const input = screen.getByLabelText('数字编辑')

    fireEvent.change(input, { target: { value: '12a3' } })
    expect(input).toHaveValue('123')

    fireEvent.change(input, { target: { value: '1.236' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith(1.24, 'down')
  })

  it('空值提交 null，0 提交 0（空值与 0 区分）', () => {
    const empty = renderEditor(NUMBER_EDITOR, { value: 5 })
    const emptyInput = screen.getByLabelText('数字编辑')
    fireEvent.change(emptyInput, { target: { value: '' } })
    fireEvent.keyDown(emptyInput, { key: 'Tab' })
    expect(empty.onCommit).toHaveBeenCalledWith(null, 'right')

    const zero = renderEditor(NUMBER_EDITOR, { value: 5 })
    const zeroInput = screen.getAllByLabelText('数字编辑')[1] as HTMLInputElement
    fireEvent.change(zeroInput, { target: { value: '0' } })
    fireEvent.keyDown(zeroInput, { key: 'Enter' })
    expect(zero.onCommit).toHaveBeenCalledWith(0, 'down')
  })

  it('非法输入行内报错且不提交；Esc 取消不落盘', () => {
    const { onCommit, onReject } = renderEditor(NUMBER_EDITOR, { value: null })
    const input = screen.getByLabelText('数字编辑')

    fireEvent.change(input, { target: { value: '-' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('请输入有效数字')
    expect(input).toHaveValue('-')

    fireEvent.keyDown(input, { key: 'Escape' })
    expect(onReject).toHaveBeenCalledTimes(1)
  })
})

describe('select 编辑器', () => {
  const baseConfig = {
    option_list: [
      { id: 'opt_1', name: '进行中', color: 'blue' },
      { id: 'opt_2', name: '完成', color: 'green' },
    ],
    allow_create: false,
  }

  it('搜索无匹配且 allow_create=false：提示联系管理员且不可新建', () => {
    const createOption = vi.fn(async () => 'opt_new')
    const { onCommit } = renderEditor(SELECT_EDITOR, {
      value: null,
      config: { ...baseConfig, createOption },
    })
    const input = screen.getByLabelText('搜索选项')

    fireEvent.change(input, { target: { value: '暂停' } })
    expect(screen.getByText('无匹配选项，请联系表格管理员添加')).toBeInTheDocument()
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(createOption).not.toHaveBeenCalled()
    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('无匹配选项，请联系表格管理员添加')
  })

  it('allow_create=true：调用 H-40 增选项回调后提交新 option_id', async () => {
    const createOption = vi.fn(async () => 'opt_new')
    const { onCommit } = renderEditor(SELECT_EDITOR, {
      value: null,
      config: { ...baseConfig, allow_create: true, createOption },
    })
    const input = screen.getByLabelText('搜索选项')

    fireEvent.change(input, { target: { value: '暂停' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(createOption).toHaveBeenCalledWith('暂停')
    await waitFor(() => expect(onCommit).toHaveBeenCalledWith('opt_new', 'down'))
  })

  it('按选项名匹配并提交 option_id', () => {
    const { onCommit } = renderEditor(SELECT_EDITOR, { value: null, config: baseConfig })
    const input = screen.getByLabelText('搜索选项')

    fireEvent.change(input, { target: { value: '完成' } })
    fireEvent.keyDown(input, { key: 'Tab', shiftKey: false })

    expect(onCommit).toHaveBeenCalledWith('opt_2', 'right')
  })
})

describe('multiselect 编辑器', () => {
  const config = {
    option_list: [
      { id: 'opt_1', name: '进行中', color: 'blue' },
      { id: 'opt_2', name: '完成', color: 'green' },
    ],
  }

  it('勾选/逐个移除后提交数组', () => {
    const { onCommit } = renderEditor(MULTISELECT_EDITOR, { value: ['opt_1'], config })

    fireEvent.click(screen.getByRole('option', { name: /完成/ }))
    fireEvent.click(screen.getByRole('button', { name: '移除选项：进行中' }))
    fireEvent.keyDown(screen.getByLabelText('搜索选项'), { key: 'Enter' })

    expect(onCommit).toHaveBeenCalledWith(['opt_2'], 'down')
  })
})

describe('date 编辑器', () => {
  it('键盘输入三种格式可提交；非法/早于 1900 行内报错不提交', () => {
    const { onCommit } = renderEditor(DATE_EDITOR, {
      value: new Date(2024, 0, 5).getTime(),
      config: { format: 'YYYY-MM-DD', include_time: false },
    })
    const input = screen.getByLabelText('日期编辑')
    expect(input).toHaveValue('2024-01-05')

    fireEvent.change(input, { target: { value: '1899-12-31' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('请输入有效日期')

    fireEvent.change(input, { target: { value: '2024年3月8日' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledTimes(1)
    const ms = (onCommit.mock.calls[0]?.[0] as number) ?? 0
    expect(new Date(ms).getMonth()).toBe(2)
    expect(new Date(ms).getDate()).toBe(8)
  })

  it('自实现日历：点击日期提交；1900 年前的日期禁用', () => {
    const { onCommit } = renderEditor(DATE_EDITOR, {
      value: new Date(1900, 0, 1).getTime(),
      config: { include_time: false, format: 'YYYY-MM-DD' },
    })

    expect(screen.getByRole('dialog', { name: '日期选择器' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '1899-12-31' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: '1900-01-10' }))
    expect(onCommit).toHaveBeenCalled()
    const ms = (onCommit.mock.calls[0]?.[0] as number) ?? 0
    expect(new Date(ms).getDate()).toBe(10)
    expect(onCommit.mock.calls[0]?.[1]).toBe('none')
  })

  it('include_time=true 展示时间输入', () => {
    renderEditor(DATE_EDITOR, {
      value: new Date(2024, 0, 5, 9, 30).getTime(),
      config: { include_time: true, format: 'YYYY-MM-DD' },
    })
    expect(screen.getByLabelText('时间（HH:mm）')).toBeInTheDocument()
  })
})

describe('person 编辑器', () => {
  const users = {
    usr_1: { name: '张三' },
    usr_2: { name: '李四' },
    usr_3: { name: '已停用', disabled: true },
  }

  it('候选来自注入的 H-63 协作者；单人选择提交 user_id', () => {
    const { onCommit } = renderEditor(PERSON_EDITOR, {
      value: null,
      config: { allow_multiple: false, users },
    })

    const panel = screen.getByRole('listbox', { name: '人员候选' })
    expect(within(panel).getByRole('option', { name: /张三/ })).toBeInTheDocument()
    fireEvent.click(within(panel).getByRole('option', { name: /李四/ }))
    fireEvent.keyDown(screen.getByLabelText('搜索人员'), { key: 'Enter' })

    expect(onCommit).toHaveBeenCalledWith('usr_2', 'down')
  })

  it('allow_multiple=true 提交 id 数组；停用成员可见并可移除', () => {
    const { onCommit } = renderEditor(PERSON_EDITOR, {
      value: ['usr_1'],
      config: { allow_multiple: true, users },
    })

    fireEvent.click(screen.getByRole('option', { name: /李四/ }))
    fireEvent.click(screen.getByRole('button', { name: '移除人员：张三' }))
    fireEvent.keyDown(screen.getByLabelText('搜索人员'), { key: 'Enter' })

    expect(onCommit).toHaveBeenCalledWith(['usr_2'], 'down')
    expect(screen.getByRole('option', { name: /已停用（已停用成员）/ })).toBeInTheDocument()
  })
})

describe('relation 编辑器', () => {
  const labels = { rec_9: '客户甲', rec_10: '客户乙' }

  it('目标记录搜索面板：多选提交 record_id 数组', () => {
    const { onCommit } = renderEditor(RELATION_EDITOR, {
      value: null,
      config: { allow_multiple: true, labels },
    })

    fireEvent.click(screen.getByRole('option', { name: /客户甲/ }))
    fireEvent.click(screen.getByRole('option', { name: /客户乙/ }))
    fireEvent.keyDown(screen.getByLabelText('搜索关联记录'), { key: 'Enter' })

    expect(onCommit).toHaveBeenCalledWith(['rec_9', 'rec_10'], 'down')
  })
})

describe('url 编辑器', () => {
  it('自动补 https:// 并提交 URL + 显示文本', () => {
    const { onCommit } = renderEditor(URL_EDITOR, { value: null })

    fireEvent.change(screen.getByLabelText('链接地址'), { target: { value: 'example.com' } })
    fireEvent.change(screen.getByLabelText('显示文本'), { target: { value: '示例' } })
    fireEvent.keyDown(screen.getByLabelText('显示文本'), { key: 'Enter' })

    expect(onCommit).toHaveBeenCalledWith(
      { url: 'https://example.com', display_text: '示例' },
      'down',
    )
  })

  it('非 http/https 协议行内报错且不提交', () => {
    const { onCommit } = renderEditor(URL_EDITOR, { value: null })

    fireEvent.change(screen.getByLabelText('链接地址'), {
      target: { value: 'javascript:alert(1)' },
    })
    fireEvent.keyDown(screen.getByLabelText('链接地址'), { key: 'Enter' })

    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('请输入合法的 http/https 链接')
  })
})
