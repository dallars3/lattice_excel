// number 演示渲染器 RTL 冒烟（票 08）：格式化结果外部语义可断言（可见文本）、
// 对齐/裁剪语义元数据正确、空值不崩溃；并经注册表取用冒烟（框架 × 演示接线）。
import '@testing-library/jest-dom/vitest'

import { createElement } from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { createCellRegistry } from '../registry'
import { NUMBER_RENDERER } from './number'
import styles from './number.module.css'

function renderCell(value: unknown, config: unknown): void {
  render(createElement(NUMBER_RENDERER.component, { value, config }))
}

describe('NUMBER_RENDERER（票 08 number 演示）', () => {
  it('定义语义元数据：右对齐 + 单行省略裁剪（技术方案 6.4 渲染器语义）', () => {
    expect(NUMBER_RENDERER.align).toBe('right')
    expect(NUMBER_RENDERER.clip).toBe('ellipsis')
  })

  it.each([
    { value: 1234567, config: { thousands: true }, expected: '1,234,567' },
    { value: 1234.5678, config: { precision: 2, thousands: true }, expected: '1,234.57' },
    { value: 0, config: {}, expected: '0' },
    { value: -98765.4, config: { precision: 1, thousands: true }, expected: '-98,765.4' },
  ])('按配置渲染格式化结果：$value → "$expected"', ({ value, config, expected }) => {
    renderCell(value, config)

    expect(screen.getByText(expected)).toBeInTheDocument()
  })

  it('单元格元素应用样式模块（省略/右对齐样式的挂载点）', () => {
    const { container } = render(createElement(NUMBER_RENDERER.component, { value: 1, config: undefined }))

    expect(container.querySelector('span')).toHaveClass(styles.number)
  })

  it('空值（null）渲染为空内容，不崩溃', () => {
    const { container } = render(createElement(NUMBER_RENDERER.component, { value: null, config: undefined }))

    expect(container.querySelector('span')?.textContent).toBe('')
  })

  it('注册表接线：注册为 "number" 后经 queryRenderer 取用渲染一致', () => {
    const registry = createCellRegistry()
    registry.registerRenderer('number', NUMBER_RENDERER)
    render(createElement(registry.queryRenderer('number').component, { value: 1234.5, config: { precision: 1 } }))

    expect(screen.getByText('1234.5')).toBeInTheDocument()
  })
})
