// CellRenderer / CellEditor 注册表框架测试（票 08；技术方案 6.4 为接口定型唯一事实源）。
// 框架契约：两注册表 register/query 往返、去重（重复注册同类型 = 后注册覆盖并返回旧定义）、
// 未知类型查询返回兜底（渲染器安全显示原始值文本 / 编辑器通用字符串编辑，RTL 冒烟断言）、
// 编辑器统一生命周期三键解析（Enter/Tab/Esc 行为一致）与统一提交流（validate 失败阻止提交）。
import '@testing-library/jest-dom/vitest'

import { createElement } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { FALLBACK_EDITOR, FALLBACK_RENDERER } from './renderers/fallback'
import {
  commitEditorDraft,
  createCellRegistry,
  resolveEditorKey,
  type CellEditorDef,
  type CellEditorLifecycle,
  type CellRendererDef,
  type EditorKeyAction,
  type EditorKeyOptions,
} from './registry'

// 桩定义：框架行为测试不依赖任何具体演示实现
const stubRenderer = (name: string): CellRendererDef => ({
  align: 'left',
  clip: 'visible',
  component: function StubCell() {
    return <span>{name}</span>
  },
})

const stubEditor = (name: string): CellEditorDef => ({
  lifecycle: {
    begin: () => 'draft',
    input: ({ input }) => String(input),
    validate: () => null,
    commit: ({ draft }) => draft,
  },
  component: function StubEditor() {
    return <span>{name}</span>
  },
})

describe('createCellRegistry（票 08 框架）', () => {
  describe('渲染器注册表（renderers[fieldType]）', () => {
    it('注册后按类型键查回同一渲染器定义（register/query 往返）', () => {
      const registry = createCellRegistry()
      const def = stubRenderer('text-stub')

      expect(registry.registerRenderer('text', def)).toBeUndefined() // 首次注册：无旧定义
      expect(registry.queryRenderer('text')).toBe(def)
    })

    it('重复注册同类型：后注册覆盖（last-wins），register 返回被替换的旧定义', () => {
      const registry = createCellRegistry()
      const first = stubRenderer('first')
      const second = stubRenderer('second')

      registry.registerRenderer('number', first)
      expect(registry.registerRenderer('number', second)).toBe(first)
      expect(registry.queryRenderer('number')).toBe(second)
    })

    it('类型键精确匹配：大小写不同视为不同类型（不归一化）', () => {
      const registry = createCellRegistry()
      const def = stubRenderer('lower')

      registry.registerRenderer('text', def)
      expect(registry.queryRenderer('Text')).toBe(FALLBACK_RENDERER)
      expect(registry.queryRenderer('text')).toBe(def)
    })

    it('未知类型查询返回兜底渲染器（同一内置兜底定义）', () => {
      const registry = createCellRegistry()

      expect(registry.queryRenderer('mystery-type')).toBe(FALLBACK_RENDERER)
      expect(registry.queryRenderer('')).toBe(FALLBACK_RENDERER)
    })

    it.each([
      { value: '原始文本', expected: '原始文本' },
      { value: 42, expected: '42' },
      { value: true, expected: 'true' },
    ])('兜底渲染器安全显示原始值文本：value=$value → "$expected"', ({ value, expected }) => {
      const registry = createCellRegistry()
      render(createElement(registry.queryRenderer('mystery').component, { value, config: undefined }))

      expect(screen.getByText(expected)).toBeInTheDocument()
    })

    it('兜底渲染器对空单元格（null）显示空文本，不抛错', () => {
      const registry = createCellRegistry()
      const { container } = render(
        createElement(registry.queryRenderer('mystery').component, { value: null, config: undefined }),
      )

      expect(container.querySelector('span')?.textContent).toBe('')
    })
  })

  describe('编辑器注册表（editors[fieldType]，技术方案 6.4 两注册表）', () => {
    it('注册后按类型键查回同一编辑器定义（register/query 往返）', () => {
      const registry = createCellRegistry()
      const def = stubEditor('text-editor-stub')

      expect(registry.registerEditor('text', def)).toBeUndefined()
      expect(registry.queryEditor('text')).toBe(def)
    })

    it('重复注册同类型：后注册覆盖（last-wins），register 返回被替换的旧定义', () => {
      const registry = createCellRegistry()
      const first = stubEditor('first')
      const second = stubEditor('second')

      registry.registerEditor('select', first)
      expect(registry.registerEditor('select', second)).toBe(first)
      expect(registry.queryEditor('select')).toBe(second)
    })

    it('未知类型查询返回兜底编辑器（同一内置兜底定义）', () => {
      const registry = createCellRegistry()

      expect(registry.queryEditor('mystery-type')).toBe(FALLBACK_EDITOR)
    })

    it('兜底编辑器：通用字符串编辑——非字符串值 String() 化进入草稿，Enter 提交原始字符串形态', () => {
      const onCommit = vi.fn()
      const onReject = vi.fn()
      const registry = createCellRegistry()
      render(
        createElement(registry.queryEditor('mystery').component, {
          value: 7,
          config: undefined,
          onCommit,
          onReject,
        }),
      )
      fireEvent.change(screen.getByRole('textbox'), { target: { value: '77' } })
      fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

      expect(onCommit).toHaveBeenCalledWith('77', 'down')
      expect(onReject).not.toHaveBeenCalled()
    })

    it('兜底编辑器：Esc 取消（reject 分支），不产出提交', () => {
      const onCommit = vi.fn()
      const onReject = vi.fn()
      const registry = createCellRegistry()
      render(
        createElement(registry.queryEditor('mystery').component, {
          value: 'v',
          config: undefined,
          onCommit,
          onReject,
        }),
      )
      fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' })

      expect(onReject).toHaveBeenCalledTimes(1)
      expect(onCommit).not.toHaveBeenCalled()
    })
  })

  describe('编辑器统一生命周期（begin → input → validate → commit/reject）', () => {
    it.each([
      { key: 'Enter', options: undefined, expected: { kind: 'commit', move: 'down' } },
      { key: 'Enter', options: { enterMove: 'none' }, expected: { kind: 'commit', move: 'none' } },
      { key: 'Tab', options: undefined, expected: { kind: 'commit', move: 'right' } },
      { key: 'Tab', options: { shiftKey: true }, expected: { kind: 'commit', move: 'left' } },
      { key: 'Escape', options: undefined, expected: { kind: 'reject' } },
      { key: 'a', options: undefined, expected: { kind: 'pass' } },
      { key: 'ArrowDown', options: undefined, expected: { kind: 'pass' } },
    ] as ReadonlyArray<{ key: string; options?: EditorKeyOptions; expected: EditorKeyAction }>)(
      '三键解析 resolveEditorKey：$key → $expected',
      ({ key, options, expected }) => {
        expect(resolveEditorKey(key, options)).toEqual(expected)
      },
    )

    it('统一提交流：validate 通过 → commit 产出提交值并携带移动语义', () => {
      const lifecycle: CellEditorLifecycle<string, string, { max: number }, string> = {
        begin: ({ value }) => String(value ?? ''),
        input: ({ input }) => String(input),
        validate: ({ draft, config }) => (draft.length > config.max ? '内容超出上限' : null),
        commit: ({ draft }) => `提交:${draft}`,
      }

      expect(commitEditorDraft(lifecycle, { draft: 'ab', config: { max: 3 } }, 'down')).toEqual({
        ok: true,
        value: '提交:ab',
        move: 'down',
      })
    })

    it('统一提交流：validate 失败 → 阻止提交（commit 钩子不执行）并返回可访问错误文案', () => {
      const commit = vi.fn(({ draft }: { draft: string }) => draft)
      const lifecycle: CellEditorLifecycle<string, string, { max: number }, string> = {
        begin: ({ value }) => String(value ?? ''),
        input: ({ input }) => String(input),
        validate: ({ draft, config }) => (draft.length > config.max ? '内容超出上限' : null),
        commit,
      }

      const outcome = commitEditorDraft(lifecycle, { draft: 'abcd', config: { max: 3 } }, 'right')

      expect(outcome).toEqual({ ok: false, error: '内容超出上限', move: 'right' })
      expect(commit).not.toHaveBeenCalled()
    })
  })
})
