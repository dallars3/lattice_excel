// 工作台异常态组件用例（票 09）：3001 转只读、离线只读、9001 自动重试后手动重试。
import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { AppProviders, appQueryClient } from '@/app/providers'
import { AppRoutes } from '@/app/router'
import { lookupErrorCatalog } from '@/core/http'
import { OFFLINE_MESSAGE, PERMISSION_CHANGED_MESSAGE } from '@/app/GlobalErrorBridge'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore, workspaceStore } from '@/stores'

const BASE_ID = 'base_1'
const TABLE_ID = 'tbl_1'
const VIEW_ID = 'view_1'

function rawBase(role = 'owner') {
  return {
    id: BASE_ID,
    name: '项目排期',
    table_count: 1,
    my_role: role,
    starred: false,
    last_modified_at: 1730000000000,
    last_modifier: 'Alice',
  }
}

function rawTable() {
  return {
    id: TABLE_ID,
    base_id: BASE_ID,
    name: '项目排期',
    field_count: 1,
    view_count: 1,
    created_at: '1730000000000',
  }
}

function rawView() {
  return { id: VIEW_ID, table_id: TABLE_ID, name: '表格视图', type: 'table', created_at: '0' }
}

function rawField() {
  return {
    id: 'fld_title',
    table_id: TABLE_ID,
    name: '标题',
    type: 'FIELD_TYPE_TEXT',
    config_map: {},
    is_primary: true,
    position: 0,
    description: '',
  }
}

function rawRecord(value = '原标题') {
  return {
    id: 'rec_1',
    table_id: TABLE_ID,
    cells_map: { fld_title: { string_value: value } },
    created_by: 'usr_1',
    created_at: '0',
    version: '1',
  }
}

function fail(code: number) {
  const entry = lookupErrorCatalog(code)
  return HttpResponse.json(
    { code, message: entry.title, data: null },
    { status: entry.httpStatus },
  )
}

function signIn() {
  authStore.getState().clear()
  authStore.getState().setSession({
    user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
    enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  })
}

function setOnline(online: boolean) {
  Object.defineProperty(window.navigator, 'onLine', { configurable: true, value: online })
}

function mockWorkspace(records = [rawRecord()]) {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/home',
      data: {
        recent_list: [],
        created_list: [rawBase()],
        shared_list: [],
        star_list: [],
        invitation_list: [],
      },
    }),
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/tables',
      data: { table_list: [rawTable()] },
    }),
    mockEnvelope({
      path: '*/api/v1/tables/:table_id/views',
      data: { view_list: [rawView()] },
    }),
    mockEnvelope({
      path: '*/api/v1/tables/:table_id/fields',
      data: { field_list: [rawField()] },
    }),
    mockEnvelope({
      path: '*/api/v1/views/:view_id/config',
      data: { view_config: {} },
    }),
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/records',
      data: { record_list: records, has_more: false, next_cursor: null },
    }),
  )
}

function renderWorkspace() {
  render(
    <AppProviders>
      <MemoryRouter initialEntries={[`/base/${BASE_ID}`]}>
        <AppRoutes />
      </MemoryRouter>
    </AppProviders>,
  )
}

beforeEach(() => {
  appQueryClient.clear()
  signIn()
  workspaceStore.setState({ inFlightWriteCount: 0, offline: false, forcedReadOnly: false })
  setOnline(true)
})

afterEach(() => {
  appQueryClient.clear()
  workspaceStore.setState({ forcedReadOnly: false, offline: false })
  setOnline(true)
})

describe('权限中途降级（3001）', () => {
  it('写请求返回 3001 后立即转只读并提示；新增隐藏、双击不再进入编辑', async () => {
    mockWorkspace()
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () => fail(3001)),
    )
    renderWorkspace()

    expect(await screen.findByRole('heading', { level: 1, name: '项目排期' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '+ 新增记录' })).toBeInTheDocument()

    const cell = await screen.findByTestId('cell-rec_1-fld_title')
    fireEvent.doubleClick(cell)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '新标题' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    expect(await screen.findByText(PERMISSION_CHANGED_MESSAGE)).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: '+ 新增记录' })).not.toBeInTheDocument(),
    )
    expect(screen.queryByRole('button', { name: '新建数据表' })).not.toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('textbox')).not.toBeInTheDocument())

    fireEvent.doubleClick(screen.getByTestId('cell-rec_1-fld_title'))
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })
})

describe('离线只读', () => {
  it('断网时展示黄色横幅、顶栏「离线」，并隐藏新增/编辑入口；恢复在线后复位', async () => {
    setOnline(false)
    mockWorkspace()
    renderWorkspace()

    expect(await screen.findByText(OFFLINE_MESSAGE)).toBeInTheDocument()
    expect(await screen.findByRole('heading', { level: 1, name: '项目排期' })).toBeInTheDocument()
    expect(screen.getByText('离线')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '+ 新增记录' })).not.toBeInTheDocument()

    setOnline(true)
    window.dispatchEvent(new Event('online'))
    await waitFor(() => expect(screen.queryByText(OFFLINE_MESSAGE)).not.toBeInTheDocument())
    expect(await screen.findByRole('button', { name: '+ 新增记录' })).toBeInTheDocument()
    expect(screen.getByText('已保存')).toBeInTheDocument()
  })
})

describe('服务端 5xx 自动重试（9001）', () => {
  it('读请求自动重试 3 次后转手动重试，手动点击后恢复', async () => {
    let attempts = 0
    mswServer.use(
      http.get('*/api/v1/home', () => {
        attempts += 1
        return fail(9001)
      }),
    )
    renderWorkspace()

    await waitFor(() => expect(attempts).toBe(4), { timeout: 5000 })
    expect(screen.getByText('服务暂时不可用')).toBeInTheDocument()
    const retry = screen.getByRole('button', { name: '重新加载' })

    mswServer.use(
      mockEnvelope({
        path: '/api/v1/home',
        data: {
          recent_list: [],
          created_list: [rawBase()],
          shared_list: [],
          star_list: [],
          invitation_list: [],
        },
      }),
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/tables',
        data: { table_list: [rawTable()] },
      }),
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/views',
        data: { view_list: [rawView()] },
      }),
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/fields',
        data: { field_list: [rawField()] },
      }),
      mockEnvelope({
        path: '*/api/v1/views/:view_id/config',
        data: { view_config: {} },
      }),
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/records',
        data: { record_list: [], has_more: false, next_cursor: null },
      }),
    )
    fireEvent.click(retry)

    expect(await screen.findByRole('heading', { level: 1, name: '项目排期' })).toBeInTheDocument()
  })
})
