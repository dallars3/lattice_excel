// 记录详情抽屉与工作台接线（票 08）：打开/关闭、网格保持可交互、关闭后选区不丢失。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore, workspaceStore } from '@/stores'

const BASE_ID = 'base_1'

const BASE = {
  id: BASE_ID,
  name: '项目排期',
  table_count: 1,
  my_role: 'owner',
  starred: false,
  last_modified_at: 1730000000000,
  last_modifier: 'Alice',
}

const TABLE = {
  id: 'tbl_1',
  base_id: BASE_ID,
  name: '项目排期',
  field_count: 2,
  view_count: 1,
  created_at: '0',
}

const VIEW = { id: 'view_1', table_id: 'tbl_1', name: '表格视图', type: 'table', created_at: '0' }

const FIELDS = [
  {
    id: 'fld_title',
    table_id: 'tbl_1',
    name: '标题',
    type: 'FIELD_TYPE_TEXT',
    config_map: {},
    is_primary: true,
    position: 0,
    description: '',
  },
  {
    id: 'fld_hidden',
    table_id: 'tbl_1',
    name: '隐藏说明',
    type: 'FIELD_TYPE_TEXT',
    config_map: {},
    is_primary: false,
    position: 1,
    description: '',
  },
]

const RECORDS = [
  {
    id: 'rec_1',
    table_id: 'tbl_1',
    cells_map: {
      fld_title: { string_value: '任务一' },
      fld_hidden: { string_value: '仅抽屉可见' },
    },
    created_by: 'usr_1',
    created_at: '0',
    version: '1',
  },
  {
    id: 'rec_2',
    table_id: 'tbl_1',
    cells_map: { fld_title: { string_value: '任务二' } },
    created_by: 'usr_1',
    created_at: '0',
    version: '1',
  },
]

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

function signIn(role = 'owner') {
  authStore.getState().clear()
  authStore.getState().setSession({
    user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
    enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  })
  return role
}

function mockWorkspace(role: string) {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/home',
      data: {
        recent_list: [],
        created_list: [{ ...BASE, my_role: role }],
        shared_list: [],
        star_list: [],
        invitation_list: [],
      },
    }),
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/tables',
      data: { table_list: [TABLE] },
    }),
    mockEnvelope({
      path: '*/api/v1/tables/:table_id/views',
      data: { view_list: [VIEW] },
    }),
    http.get('*/api/v1/views/:view_id/config', () =>
      ok({
        view_config: {
          hidden_field_list: ['fld_hidden'],
        },
      }),
    ),
    mockEnvelope({ path: '*/api/v1/tables/:table_id/fields', data: { field_list: FIELDS } }),
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/records',
      data: { record_list: RECORDS, has_more: false, next_cursor: null },
    }),
    http.get('*/api/v1/records/:record_id', ({ params }) => {
      const row = RECORDS.find((candidate) => candidate.id === String(params.record_id))
      return row === undefined ? ok({}) : ok({ record: row })
    }),
  )
}

function renderWorkspace() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/base/${BASE_ID}`]}>
        <AppRoutes />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('工作台 ↔ 记录详情抽屉（票 08）', () => {
  beforeEach(() => {
    workspaceStore.setState({ inFlightWriteCount: 0, offline: false, fileMetaMap: {} })
  })

  it('行首展开按钮打开右侧抽屉；关闭后网格原选中位置不丢失', async () => {
    signIn()
    mockWorkspace('owner')
    renderWorkspace()

    const firstCell = await screen.findByTestId('cell-rec_1-fld_title')
    fireEvent.click(firstCell)
    expect(screen.getByTestId('cell-rec_1-fld_title')).toHaveAttribute('aria-selected', 'true')

    fireEvent.click(screen.getByRole('button', { name: '展开记录 rec_1' }))
    expect(await screen.findByRole('dialog', { name: '记录详情' })).toBeInTheDocument()
    // 抽屉内展示当前视图隐藏字段
    expect(await screen.findByTestId('drawer-field-fld_hidden')).toBeInTheDocument()
    expect(screen.getByText('仅抽屉可见')).toBeInTheDocument()

    // 抽屉展开时网格仍可交互：点击第二行选中
    fireEvent.click(screen.getByTestId('cell-rec_2-fld_title'))
    expect(screen.getByTestId('cell-rec_2-fld_title')).toHaveAttribute('aria-selected', 'true')

    fireEvent.click(screen.getByRole('button', { name: '关闭记录详情' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: '记录详情' })).not.toBeInTheDocument(),
    )
    expect(screen.getByTestId('cell-rec_2-fld_title')).toHaveAttribute('aria-selected', 'true')
  })

  it('可查看（viewer）不展示抽屉内编辑入口', async () => {
    signIn()
    mockWorkspace('viewer')
    renderWorkspace()

    await screen.findByTestId('cell-rec_1-fld_title')
    fireEvent.click(screen.getByRole('button', { name: '展开记录 rec_1' }))
    await screen.findByRole('dialog', { name: '记录详情' })
    expect(screen.queryByRole('button', { name: '编辑主字段：标题' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('编辑字段：隐藏说明')).not.toBeInTheDocument()
  })
})
