import '@testing-library/jest-dom/vitest'

import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { DEFAULT_NOTICE_TEXT } from '@/lib/env'

import { AppRoutes } from './router'

// 票 01 冒烟用例（票 02 调整渲染位置）：App.tsx 改为路由出口后，标题+声明文案并入 /home 首页路由，
// 断言保持不变。
describe('App 占位首页', () => {
  it('渲染产品标题与试运行声明文案', () => {
    render(
      <MemoryRouter initialEntries={['/home']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    const heading = screen.getByRole('heading', { level: 1 })
    expect(heading).toHaveTextContent('协作多维表格')
    expect(screen.getByText(DEFAULT_NOTICE_TEXT)).toBeInTheDocument()
  })
})
