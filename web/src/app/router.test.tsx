import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { buildDemoRows, DEMO_RECORDS_PATH, DEMO_ROW_TOTAL } from '@/features/grid-view/demo-table'
import { DEFAULT_NOTICE_TEXT } from '@/lib/env'
import { mockCursorRecords } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import { AppRoutes } from './router'

interface RouteCase {
  path: string
  heading: string
  extra?: string
}

// 三条占位路由（票 02 AC）：可达 + 各自渲染 HealthNotice（三处文案覆盖底线）
const ROUTE_CASES: RouteCase[] = [
  { path: '/login', heading: '登录' },
  { path: '/home', heading: '协作多维表格' },
  { path: '/base/base_001', heading: 'Base 工作台', extra: 'base_001' },
]

describe('AppRoutes 占位路由（票 02）', () => {
  it.each(ROUTE_CASES)('$path 可达且渲染占位页标题', ({ path, heading, extra }: RouteCase) => {
    render(
      <MemoryRouter initialEntries={[path]}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(heading)
    if (extra !== undefined) {
      expect(screen.getByText(extra)).toBeInTheDocument()
    }
  })

  it.each(ROUTE_CASES)('$path 渲染试运行免责声明（HealthNotice 三处强制挂载）', ({ path }: RouteCase) => {
    render(
      <MemoryRouter initialEntries={[path]}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getAllByText(DEFAULT_NOTICE_TEXT)).not.toHaveLength(0)
    expect(screen.getByRole('alert')).toHaveTextContent(DEFAULT_NOTICE_TEXT)
  })

  it('未知路径回退首页（占位内容保持在位）', () => {
    render(
      <MemoryRouter initialEntries={['/no-such-route']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格')
  })

  it('占位路由可互相导航：/home → /login → /base/:id', () => {
    render(
      <MemoryRouter initialEntries={['/home']}>
        <AppRoutes />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('link', { name: '前往登录' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('登录')

    fireEvent.click(screen.getByRole('link', { name: '前往首页' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格')

    fireEvent.click(screen.getByRole('link', { name: '打开演示 Base 工作台' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Base 工作台')

    fireEvent.click(screen.getByRole('link', { name: '返回首页' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格')
  })
})

describe('AppRoutes 演示路由（票 09）', () => {
  it('/demo/grid 可达且渲染网格演示页（主站三条路由不受影响，见上方用例）', async () => {
    mswServer.use(mockCursorRecords({ path: DEMO_RECORDS_PATH, rows: buildDemoRows(DEMO_ROW_TOTAL) }))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <MemoryRouter initialEntries={['/demo/grid']}>
        <QueryClientProvider client={queryClient}>
          <AppRoutes />
        </QueryClientProvider>
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('网格演示')
    // 经真实路由表渲染到 MSW 假数据（页面数据链路完整可达）
    await waitFor(() => expect(screen.getByText('记录 1')).toBeInTheDocument())
  })
})
