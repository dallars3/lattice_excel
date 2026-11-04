// select 演示渲染器 RTL 冒烟（票 08）：选项标签按配置色渲染——色值来自字段配置注入（不写死），
// 标签样式走 token + 注入色；未知 id 安全显示原始值；空值不崩溃。
import '@testing-library/jest-dom/vitest'

import { createElement } from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { createCellRegistry } from '../registry'
import { SELECT_RENDERER, type SelectCellConfig } from './select'
import styles from './select.module.css'

const CONFIG: SelectCellConfig = {
  options: [
    { id: 'opt-todo', label: '待办' },
    { id: 'opt-doing', label: '进行中', color: '#3370ff' },
    { id: 'opt-done', label: '已完成', color: '#2ea121' },
  ],
}

function renderCell(value: unknown, config: unknown = CONFIG): ReturnType<typeof render> {
  return render(createElement(SELECT_RENDERER.component, { value, config }))
}

describe('SELECT_RENDERER（票 08 select 演示）', () => {
  it('定义语义元数据：左对齐 + 单行省略裁剪（技术方案 6.4 渲染器语义）', () => {
    expect(SELECT_RENDERER.align).toBe('left')
    expect(SELECT_RENDERER.clip).toBe('ellipsis')
  })

  it('按选项 id 渲染选项标签文本（外部语义：可见 label）', () => {
    renderCell('opt-doing')

    expect(screen.getByText('进行中')).toBeInTheDocument()
  })

  it('选项标签应用配置注入色（色值来自字段配置，不写死）：backgroundColor 为注入值', () => {
    renderCell('opt-done')

    const tag = screen.getByText('已完成')
    expect(tag).toHaveClass(styles['select__tag'])
    expect(tag.style.backgroundColor).toBe('rgb(46, 161, 33)') // '#2ea121' 注入后的序列化形态
  })

  it('未配置颜色的选项走 token 中性标签样式（无 inline 背景）', () => {
    renderCell('opt-todo')

    const tag = screen.getByText('待办')
    expect(tag).toHaveClass(styles['select__tag'])
    expect(tag.style.backgroundColor).toBe('')
  })

  it('值不在选项列表中：安全显示原始值（不崩溃、不误配标签）', () => {
    renderCell('opt-ghost')

    expect(screen.getByText('opt-ghost')).toBeInTheDocument()
    expect(screen.queryByText('待办')).not.toBeInTheDocument()
  })

  it('空单元格（null）渲染空内容，不崩溃', () => {
    const { container } = renderCell(null)

    expect(container.querySelector('span')?.textContent).toBe('')
  })

  it('注册表接线：注册为 "select" 后经 queryRenderer 取用渲染一致', () => {
    const registry = createCellRegistry()
    registry.registerRenderer('select', SELECT_RENDERER)
    render(createElement(registry.queryRenderer('select').component, { value: 'opt-doing', config: CONFIG }))

    expect(screen.getByText('进行中').style.backgroundColor).toBe('rgb(51, 112, 255)')
  })
})
