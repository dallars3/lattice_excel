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

/** 首页聚合最小形状：/home 渲染需要四列表（票 02）。 */
const HOME = {
  recent_list: [
    {
      id: 'base_demo',
      name: '演示表格',
      table_count: 1,
      my_role: 'owner',
      starred: false,
      last_modified_at: 1730000000000,
      last_modifier: 'Alice',
    },
    {
      id: 'base_001',
      name: '项目排期',
      table_count: 1,
      my_role: 'owner',
      starred: false,
      last_modified_at: 1730000000000,
      last_modifier: 'Alice',
    },
  ],
  created_list: [],
  shared_list: [],
  star_list: [],
  invitation_list: [],
}

/** 工作台深链还需要 H-29 表列表与 H-41 视图列表（票 03）。 */
const WORKSPACE_TABLES = {
  table_list: [
    {
      id: 'tbl_demo',
      base_id: 'base_demo',
      name: '数据表 1',
      field_count: 1,
      view_count: 1,
      created_at: '1730000000000',
    },
  ],
}

const WORKSPACE_VIEWS = {
  view_list: [
    {
      id: 'view_demo',
      table_id: 'tbl_demo',
      name: '表格视图',
      type: 'table',
      created_at: '1730000000000',
    },
  ],
}

// 主站路由（票 02 AC / 票 01 认证闭环）：可达 + 各自渲染 HealthNotice。
// /home、/base/:id 经 RequireSession 守卫，用例在 HTTP 边界 mock H-04 me。
const ROUTE_CASES: RouteCase[] = [
  { path: '/login', heading: '登录' },
  { path: '/register', heading: '注册' },
  { path: '/home', heading: '协作多维表格' },
  { path: '/base/base_001', heading: '项目排期' },
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
    mswServer.use(
      mockEnvelope({ path: '/api/v1/auth/me', data: ME }),
      mockEnvelope({ path: '/api/v1/home', data: HOME }),
      mockEnvelope({ path: '*/api/v1/bases/:base_id/tables', data: WORKSPACE_TABLES }),
      mockEnvelope({ path: '*/api/v1/tables/:table_id/views', data: WORKSPACE_VIEWS }),
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/fields',
        data: {
          field_list: [
            {
              id: 'fld_primary',
              table_id: 'tbl_demo',
              name: '标题',
              type: 'FIELD_TYPE_TEXT',
              config_map: {},
              is_primary: true,
              position: 0,
              description: '',
            },
          ],
        },
      }),
    )
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

  it('主站路由可导航：/home → /base/:id 进入五段式工作台', async () => {
    renderRoutes('/home')
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )

    fireEvent.click(await screen.findByRole('button', { name: '打开多维表格：演示表格' }))
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('演示表格'),
    )
    expect(screen.getByRole('region', { name: '网格' })).toBeInTheDocument()
    expect(await screen.findByRole('tab', { name: '数据表 1' })).toBeInTheDocument()
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
