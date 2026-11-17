// 8 类单元格编辑器（票 06）行为测试：注册表、三键语义、行内校验、各类型专有控件。
// 只在编辑器组件公开接缝上断言（输入 → 提交值/错误），不触达内部状态命名。
import '@testing-library/jest-dom/vitest'

import { createElement } from 'react'
import { act, createEvent, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
import { EDITOR_POPUP_ATTR } from './editor-popup'

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

  it('失效选项 id 标签同兜底：显示「已删除的选项」灰色标签而非原始 ID，仍可移除', () => {
    const { onCommit } = renderEditor(MULTISELECT_EDITOR, { value: ['opt_ghost', 'opt_1'], config })

    // 票 04：失效选项 ID 不再显示原始长串，编辑器标签同文案同样式
    expect(screen.queryByText('opt_ghost')).not.toBeInTheDocument()
    const fallback = screen.getByText('已删除的选项')
    expect(fallback.className).toContain('tagMissing')
    expect(screen.getByRole('button', { name: '移除选项：已删除的选项' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '移除选项：进行中' })).toBeInTheDocument()

    // 兜底标签可正常移除，提交集合保留其余已选项
    fireEvent.click(screen.getByRole('button', { name: '移除选项：已删除的选项' }))
    fireEvent.keyDown(screen.getByLabelText('搜索选项'), { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith(['opt_1'], 'down')
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

// -------------------------------------------------------------------------------------------
// 弹层 portal 化（票 01 / BUG-06）：面板/日历/行内错误渲染到 document.body，
// 脱离网格单元格 overflow:hidden 裁剪上下文。挂载级断言弹层节点的挂载位置。
// -------------------------------------------------------------------------------------------

/** 把编辑器渲染进 overflow:hidden 裁剪容器（模拟网格单元格），返回该容器。 */
function renderInClippingWrapper(def: CellEditorDef, options: RenderEditorOptions = {}) {
  const { onCommit, onReject } = { onCommit: vi.fn(), onReject: vi.fn() }
  const harness = render(
    <div data-testid="clip-wrapper" style={{ overflow: 'hidden' }}>
      {createElement(def.component, {
        value: options.value ?? null,
        config: options.config,
        initialInput: options.initialInput,
        onCommit,
        onReject,
      })}
    </div>,
  )
  const wrapper = harness.getByTestId('clip-wrapper')
  return { onCommit, onReject, wrapper, unmount: harness.unmount }
}

function expectPortaledOutsideWrapper(node: HTMLElement, wrapper: HTMLElement) {
  // 弹层根节点直接挂在 document.body 下，且不属于裁剪容器子树。
  expect(node.getAttribute(EDITOR_POPUP_ATTR)).toBe('')
  expect(node.parentElement).toBe(document.body)
  expect(wrapper.contains(node)).toBe(false)
}

describe('弹层 portal 化（票 01 / BUG-06）', () => {
  it('select 选项面板 portal 到 body，不在溢出裁剪容器内', () => {
    const { wrapper } = renderInClippingWrapper(SELECT_EDITOR, {
      value: null,
      config: {
        option_list: [
          { id: 'opt_1', name: '进行中', color: 'blue' },
          { id: 'opt_2', name: '完成', color: 'green' },
        ],
        allow_create: false,
      },
    })

    expectPortaledOutsideWrapper(screen.getByRole('listbox', { name: '选项列表' }), wrapper)
  })

  it('multiselect / person / relation 面板同样 portal 到 body', () => {
    const multiselect = renderInClippingWrapper(MULTISELECT_EDITOR, {
      value: null,
      config: {
        option_list: [{ id: 'opt_1', name: '进行中', color: 'blue' }],
      },
    })
    expectPortaledOutsideWrapper(screen.getByRole('listbox', { name: '选项列表' }), multiselect.wrapper)
    multiselect.unmount()

    const users = { usr_1: { name: '张三' } }
    const person = renderInClippingWrapper(PERSON_EDITOR, {
      value: null,
      config: { allow_multiple: true, users },
    })
    expectPortaledOutsideWrapper(screen.getByRole('listbox', { name: '人员候选' }), person.wrapper)
    person.unmount()

    const relation = renderInClippingWrapper(RELATION_EDITOR, {
      value: null,
      config: { allow_multiple: true, labels: { rec_9: '客户甲' } },
    })
    expectPortaledOutsideWrapper(
      screen.getByRole('listbox', { name: '关联记录候选' }),
      relation.wrapper,
    )
  })

  it('date 日历 portal 到 body，不在溢出裁剪容器内；点选日期仍提交', () => {
    const { onCommit, wrapper } = renderInClippingWrapper(DATE_EDITOR, {
      value: new Date(2024, 0, 5).getTime(),
      config: { format: 'YYYY-MM-DD', include_time: false },
    })

    const dialog = screen.getByRole('dialog', { name: '日期选择器' })
    expectPortaledOutsideWrapper(dialog, wrapper)

    fireEvent.click(screen.getByRole('button', { name: '2024-01-10' }))
    expect(onCommit).toHaveBeenCalled()
    const ms = (onCommit.mock.calls[0]?.[0] as number) ?? 0
    expect(new Date(ms).getDate()).toBe(10)
  })

  it('校验失败的行内错误提示 portal 到 body，不被裁剪容器包含', () => {
    const { onCommit, wrapper } = renderInClippingWrapper(NUMBER_EDITOR, { value: null })
    const input = screen.getByLabelText('数字编辑')

    fireEvent.change(input, { target: { value: '-' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onCommit).not.toHaveBeenCalled()
    expectPortaledOutsideWrapper(screen.getByRole('alert'), wrapper)
  })

  it('弹层在锚元素（编辑器宿主）所在单元格外的 body 上仍可点选选项（选中态生效）', () => {
    renderInClippingWrapper(MULTISELECT_EDITOR, {
      value: null,
      config: {
        option_list: [
          { id: 'opt_1', name: '进行中', color: 'blue' },
          { id: 'opt_2', name: '完成', color: 'green' },
        ],
      },
    })

    fireEvent.click(screen.getByRole('option', { name: /完成/ }))
    expect(screen.getByRole('option', { name: /完成/ })).toHaveAttribute('aria-selected', 'true')
  })
})

// -------------------------------------------------------------------------------------------
// 下拉编辑器点击选中与提交链（票 02 / BUG-01+BUG-04 网格部分）：单选点击即提交收起、
// 多选点击勾选保持打开、搜索词回车先匹配不提交旧集合、方向键高亮（aria-activedescendant）
// 回车选中高亮项、选项按钮按下不夺走输入框焦点。断言只落在编辑器公开接缝
// （onCommit/onReject/aria 状态），jsdom 无原生点击抢焦，焦点转移以 option.focus() 显式模拟。
// -------------------------------------------------------------------------------------------

/** 模拟真实浏览器点击选项的焦点链：mousedown 抢焦（显式 focus）→ click。 */
function clickOption(option: HTMLElement) {
  act(() => {
    option.focus()
  })
  fireEvent.click(option)
}

describe('下拉提交链（票 02 / BUG-01+BUG-04）', () => {
  const selectConfig = {
    option_list: [
      { id: 'opt_1', name: '进行中', color: 'blue' },
      { id: 'opt_2', name: '完成', color: 'green' },
    ],
    allow_create: false,
  }

  it('select 点击选项即提交（此前只改本地草稿从不提交）', () => {
    const { onCommit } = renderEditor(SELECT_EDITOR, { value: null, config: selectConfig })
    act(() => {
      screen.getByLabelText('搜索选项').focus()
    })

    clickOption(screen.getByRole('option', { name: /完成/ }))

    expect(onCommit).toHaveBeenCalledWith('opt_2', 'none')
  })

  it('select 选项按钮按下不夺走输入框焦点（pointerdown preventDefault）', () => {
    renderEditor(SELECT_EDITOR, { value: null, config: selectConfig })
    const option = screen.getByRole('option', { name: /完成/ })

    const event = createEvent.pointerDown(option)
    fireEvent(option, event)

    expect(event.defaultPrevented).toBe(true)
  })

  it('select 方向键移动高亮（aria-activedescendant）、回车选中高亮项', () => {
    const { onCommit } = renderEditor(SELECT_EDITOR, { value: null, config: selectConfig })
    const input = screen.getByLabelText('搜索选项')

    expect(input.getAttribute('aria-activedescendant')).toBeNull()
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    const first = screen.getByRole('option', { name: /进行中/ })
    expect(input.getAttribute('aria-activedescendant')).toBe(first.getAttribute('id'))

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    const second = screen.getByRole('option', { name: /完成/ })
    expect(input.getAttribute('aria-activedescendant')).toBe(second.getAttribute('id'))

    fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(input.getAttribute('aria-activedescendant')).toBe(first.getAttribute('id'))

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith('opt_1', 'down')
  })

  it('select 方向键高亮后 Esc 取消不落盘', () => {
    const { onCommit, onReject } = renderEditor(SELECT_EDITOR, { value: null, config: selectConfig })
    const input = screen.getByLabelText('搜索选项')

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Escape' })

    expect(onCommit).not.toHaveBeenCalled()
    expect(onReject).toHaveBeenCalledTimes(1)
  })

  it('multiselect 点击为勾选切换且编辑器保持打开（不提交）', () => {
    const { onCommit } = renderEditor(MULTISELECT_EDITOR, { value: null, config: selectConfig })

    clickOption(screen.getByRole('option', { name: /进行中/ }))
    expect(screen.getByRole('option', { name: /进行中/ })).toHaveAttribute('aria-selected', 'true')
    expect(onCommit).not.toHaveBeenCalled()

    clickOption(screen.getByRole('option', { name: /进行中/ }))
    expect(screen.getByRole('option', { name: /进行中/ })).toHaveAttribute('aria-selected', 'false')
    expect(onCommit).not.toHaveBeenCalled()
  })

  it('multiselect 搜索词+回车：先匹配勾选且编辑器保持打开，不提交旧集合（顺序缺陷修复）', () => {
    const { onCommit } = renderEditor(MULTISELECT_EDITOR, { value: null, config: selectConfig })
    const input = screen.getByLabelText('搜索选项')

    fireEvent.change(input, { target: { value: '完成' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByRole('option', { name: /完成/ })).toHaveAttribute('aria-selected', 'true')
    expect(input).toHaveValue('')
    expect(input).toBeInTheDocument()
  })

  it('multiselect 方向键高亮 + 回车勾选高亮项（保持打开）；再次回车提交已选集合', () => {
    const { onCommit } = renderEditor(MULTISELECT_EDITOR, { value: null, config: selectConfig })
    const input = screen.getByLabelText('搜索选项')

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(screen.getByRole('option', { name: /完成/ })).toHaveAttribute('aria-selected', 'true')
    expect(onCommit).not.toHaveBeenCalled()

    // 高亮复位后回车 = 提交已选集合（避免连按两次回车把刚勾选项又勾掉）
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith(['opt_2'], 'down')
  })

  it('person 单选：点击候选即提交并收起', () => {
    const users = { usr_1: { name: '张三' }, usr_2: { name: '李四' } }
    const { onCommit } = renderEditor(PERSON_EDITOR, {
      value: null,
      config: { allow_multiple: false, users },
    })

    clickOption(within(screen.getByRole('listbox', { name: '人员候选' })).getByRole('option', { name: /李四/ }))

    expect(onCommit).toHaveBeenCalledWith('usr_2', 'none')
  })

  it('person 多选：点击勾选保持打开；方向键高亮回车勾选、再回车提交集合', () => {
    const users = { usr_1: { name: '张三' }, usr_2: { name: '李四' } }
    const { onCommit } = renderEditor(PERSON_EDITOR, {
      value: null,
      config: { allow_multiple: true, users },
    })
    const input = screen.getByLabelText('搜索人员')

    clickOption(screen.getByRole('option', { name: /张三/ }))
    expect(screen.getByRole('option', { name: /张三/ })).toHaveAttribute('aria-selected', 'true')
    expect(onCommit).not.toHaveBeenCalled()

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(screen.getByRole('option', { name: /李四/ })).toHaveAttribute('aria-selected', 'true')

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith(['usr_1', 'usr_2'], 'down')
  })

  it('relation 单选：点击候选即提交并收起', () => {
    const { onCommit } = renderEditor(RELATION_EDITOR, {
      value: null,
      config: { allow_multiple: false, labels: { rec_9: '客户甲', rec_10: '客户乙' } },
    })

    clickOption(
      within(screen.getByRole('listbox', { name: '关联记录候选' })).getByRole('option', {
        name: /客户乙/,
      }),
    )

    expect(onCommit).toHaveBeenCalledWith('rec_10', 'none')
  })

  it('relation 多选：搜索词+回车只勾选匹配项不提交旧集合', () => {
    const { onCommit } = renderEditor(RELATION_EDITOR, {
      value: null,
      config: { allow_multiple: true, labels: { rec_9: '客户甲', rec_10: '客户乙' } },
    })
    const input = screen.getByLabelText('搜索关联记录')

    fireEvent.change(input, { target: { value: '客户乙' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.getByRole('option', { name: /客户乙/ })).toHaveAttribute('aria-selected', 'true')
  })
})
