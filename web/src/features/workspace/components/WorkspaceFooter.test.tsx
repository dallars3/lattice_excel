// 底栏计数测试（票 05 / G-3）：展示已加载 n 条；has_more 时 n+，不虚构总数。
import '@testing-library/jest-dom/vitest'

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { WorkspaceFooter } from './WorkspaceFooter'

describe('WorkspaceFooter（票 05 G-3 口径）', () => {
  it('无更多页：已加载 n 条', () => {
    render(<WorkspaceFooter loadedCount={20} hasMore={false} />)
    expect(screen.getByTestId('footer-record-count')).toHaveTextContent('已加载 20 条')
  })

  it('仍有下一页：已加载 n+ 条', () => {
    render(<WorkspaceFooter loadedCount={20} hasMore />)
    expect(screen.getByTestId('footer-record-count')).toHaveTextContent('已加载 20+ 条')
  })

  it('默认 0 条（首屏未加载）', () => {
    render(<WorkspaceFooter />)
    expect(screen.getByTestId('footer-record-count')).toHaveTextContent('已加载 0 条')
  })
})
