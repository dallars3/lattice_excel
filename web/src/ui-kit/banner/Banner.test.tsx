import '@testing-library/jest-dom/vitest'

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Banner, type BannerTone } from './Banner'

describe('ui-kit/Banner', () => {
  it.each([
    { tone: 'info' as const, role: 'status' },
    { tone: 'warning' as const, role: 'alert' },
  ])('tone=$tone 以 role=$role 呈现语义并渲染文案位', ({ tone, role }: { tone: BannerTone; role: string }) => {
    render(<Banner tone={tone}>离线只读中，恢复后自动重传</Banner>)

    expect(screen.getByRole(role)).toHaveTextContent('离线只读中，恢复后自动重传')
  })

  it('缺省 tone 为 info（role=status）', () => {
    render(<Banner>信息横幅</Banner>)

    expect(screen.getByRole('status')).toHaveTextContent('信息横幅')
  })

  it('缺省渲染内置内联 SVG 图标（无外部资源）', () => {
    const { container } = render(<Banner>带默认图标的信息</Banner>)

    expect(container.querySelector('svg')).toBeInTheDocument()
  })

  it('图标位可传入自定义内容并渲染', () => {
    render(
      <Banner tone="warning" icon={<span>注意</span>}>
        警告横幅
      </Banner>,
    )

    expect(screen.getByText('注意')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('警告横幅')
  })
})
