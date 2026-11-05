import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { buildDemoRows, DEMO_RECORDS_PATH, DEMO_ROW_TOTAL } from '@/features/grid-view/demo-table'
import { DEFAULT_NOTICE_TEXT } from '@/lib/env'
import { mockCursorRecords, mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore } from '@/stores'

import { AppRoutes } from './router'

interface RouteCase {
  path: string
  heading: string
  extra?: string
}

const ME = {
  user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
  enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  my_role: 'owner',
}

// 主站路由（票 02 AC / 票 01 认证闭环）：可达 + 各自渲染 HealthNotice。
// /home、/base/:id 经 RequireSession 守卫，用例在 HTTP 边界 mock H-04 me。
const ROUTE_CASES: RouteCase[] = [
  { path: '/login', heading: '登录' },
  { path: '/register', heading: '注册' },
  { path: '/home', heading: '协作多维表格' },
  { path: '/base/base_001', heading: 'Base 工作台', extra: 'base_001' },
]

function renderRoutes(entry: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[entry]}>
        <AppRoutes />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AppRoutes 主站路由（票 02 / 票 01）', () => {
  beforeEach(() => {
    authStore.getState().clear()
    mswServer.use(mockEnvelope({ path: '/api/v1/auth/me', data: ME }))
  })

  it.each(ROUTE_CASES)('$path 可达且渲染页面标题', async ({ path, heading, extra }: RouteCase) => {
    renderRoutes(path)

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(heading),
    )
    if (extra !== undefined) {
      expect(screen.getByText(extra)).toBeInTheDocument()
    }
  })

  it.each(ROUTE_CASES)('$path 渲染试运行免责声明（HealthNotice 强制挂载）', async ({ path }: RouteCase) => {
    renderRoutes(path)

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(DEFAULT_NOTICE_TEXT),
    )
    expect(screen.getAllByText(DEFAULT_NOTICE_TEXT)).not.toHaveLength(0)
  })

  it('未知路径回退首页（占位内容保持在位）', async () => {
    renderRoutes('/no-such-route')

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )
  })

  it('主站路由可互相导航：/home → /base/:id → /home', async () => {
    renderRoutes('/home')
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )

    fireEvent.click(screen.getByRole('link', { name: '打开演示 Base 工作台' }))
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Base 工作台'),
    )
    expect(screen.getByText('base_001')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('link', { name: '返回首页' }))
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )
  })
})

describe('AppRoutes 演示路由（票 09）', () => {
  it('/demo/grid 可达且渲染网格演示页（主站路由不受影响，见上方用例）', async () => {
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
