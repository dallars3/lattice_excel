import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { EmptyState } from './EmptyState'

describe('ui-kit/EmptyState', () => {
  it('渲染文案位（标题+描述），并内置内联 SVG 插图（无外部资源）', () => {
    const { container } = render(
      <EmptyState title="暂无记录" description="点击下方按钮新增一条记录" />,
    )

    expect(screen.getByText('暂无记录')).toBeInTheDocument()
    expect(screen.getByText('点击下方按钮新增一条记录')).toBeInTheDocument()
    expect(container.querySelector('svg')).toBeInTheDocument()
  })

  it('未提供描述与操作时不渲染对应区块（无按钮）', () => {
    render(<EmptyState title="暂无视图" />)

    expect(screen.getByText('暂无视图')).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('点击可选操作按钮触发回调', () => {
    const onClick = vi.fn()
    render(<EmptyState title="暂无记录" action={{ label: '新增记录', onClick }} />)

    fireEvent.click(screen.getByRole('button', { name: '新增记录' }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('插图位可传入自定义内容覆盖内置插图', () => {
    render(<EmptyState title="加载失败" illustration={<span>自定义插图</span>} />)

    expect(screen.getByText('自定义插图')).toBeInTheDocument()
  })
})
