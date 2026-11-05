// 工作台权限矩阵组件用例（票 09）：可编辑 / 可填写 / 可查看 / 外部账号与容量上限。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore, workspaceStore } from '@/stores'

const BASE_ID = 'base_1'
const TABLE_ID = 'tbl_1'
const VIEW_ID = 'view_1'

interface RawBase {
  id: string
  name: string
  table_count: number
  my_role: string
  starred: boolean
  last_modified_at: number
  last_modifier: string
}

function rawBase(role: string): RawBase {
  return {
    id: BASE_ID,
    name: '项目排期',
    table_count: 2,
    my_role: role,
    starred: false,
    last_modified_at: 1730000000000,
    last_modifier: 'Alice',
  }
}

function rawTable(id = TABLE_ID, name = '项目排期'): Record<string, unknown> {
  return {
    id,
    base_id: BASE_ID,
    name,
    field_count: 2,
    view_count: 2,
    created_at: '1730000000000',
  }
}

function rawView(id: string, name: string): Record<string, unknown> {
  return { id, table_id: TABLE_ID, name, type: 'table', created_at: '1730000000000' }
}

function rawField(
  id: string,
  name: string,
  type: string,
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    id,
    table_id: TABLE_ID,
    name,
    type,
    config_map: {},
    is_primary: false,
    position: 0,
    description: '',
    ...overrides,
  }
}

function rawRecord(id: string, createdBy: string, value: string): Record<string, unknown> {
  return {
    id,
    table_id: TABLE_ID,
    cells_map: { fld_title: { string_value: value } },
    created_by: createdBy,
    created_at: '0',
    version: '1',
  }
}

function signIn(internal = true) {
  authStore.getState().clear()
  authStore.getState().setSession(
    internal
      ? {
          user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
          enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
        }
      : { user: { id: 'usr_ext', username: 'guest' } },
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

function mockBase(role: string) {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/home',
      data: {
        recent_list: [],
        created_list: [rawBase(role)],
        shared_list: [],
        star_list: [],
        invitation_list: [],
      },
    }),
  )
}

function mockTables(tables: Record<string, unknown>[] = [rawTable()]) {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/tables',
      data: { table_list: tables },
    }),
  )
}

function mockViews() {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/tables/:table_id/views',
      data: { view_list: [rawView(VIEW_ID, '表格视图'), rawView('view_2', '看板视图')] },
    }),
  )
}

function mockFields(fields = defaultFields()) {
  mswServer.use(mockEnvelope({ path: '*/api/v1/tables/:table_id/fields', data: { field_list: fields } }))
}

function defaultFields() {
  return [
    rawField('fld_title', '标题', 'FIELD_TYPE_TEXT', { is_primary: true, position: 0 }),
    rawField('fld_amount', '金额', 'FIELD_TYPE_NUMBER', {
      position: 1,
      config_map: { format: 'decimal', decimals: 2 },
    }),
  ]
}

function mockRecords(records: Record<string, unknown>[] = []) {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/records',
      data: { record_list: records, has_more: false, next_cursor: null },
    }),
  )
}

function mockConfig() {
  mswServer.use(
    mockEnvelope({ path: '*/api/v1/views/:view_id/config', data: { view_config: {} } }),
  )
}

beforeEach(() => {
  signIn()
  workspaceStore.setState({ inFlightWriteCount: 0, offline: false, forcedReadOnly: false })
})

describe('可填写（filler）', () => {
  it('页眉提示仅显示自己创建记录，仅自己的记录可编辑，新增可见而删除/批量/结构/分享隐藏', async () => {
    mockBase('filler')
    mockTables()
    mockViews()
    mockFields()
    mockConfig()
    mockRecords([
      rawRecord('rec_own', 'usr_1', '我的记录'),
      rawRecord('rec_other', 'usr_2', '他人记录'),
    ])
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    expect(await screen.findByText('仅显示你创建的记录')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '+ 新增记录' })).toBeInTheDocument()

    expect(screen.queryByRole('button', { name: '新建数据表' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '表操作：项目排期' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新建视图' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '筛选' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '分享' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多' })).not.toBeInTheDocument()

    await screen.findByTestId('cell-rec_own-fld_title')
    expect(screen.queryByLabelText('选择记录 rec_own')).not.toBeInTheDocument()

    fireEvent.doubleClick(screen.getByTestId('cell-rec_other-fld_title'))
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()

    fireEvent.doubleClick(screen.getByTestId('cell-rec_own-fld_title'))
    expect(await screen.findByRole('textbox')).toBeInTheDocument()
  })
})

describe('可编辑（editor）', () => {
  it('可编辑单元格与视图配置，但字段删除/类型转换与顶栏重命名/复制/删除隐藏，分享位可见', async () => {
    mockBase('editor')
    mockTables()
    mockViews()
    mockFields()
    mockConfig()
    mockRecords([rawRecord('rec_1', 'usr_1', '记录一')])
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    await screen.findByRole('button', { name: '表格视图' })
    await waitFor(() => expect(screen.getByRole('button', { name: '筛选' })).toBeEnabled())
    expect(screen.queryByRole('button', { name: '新建数据表' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新建视图' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '筛选' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '分享' })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: '更多' }))
    expect(screen.getByRole('menuitem', { name: '星标' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '重命名' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '复制' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '删除' })).not.toBeInTheDocument()

    fireEvent.click(await screen.findByRole('button', { name: '字段操作：金额' }))
    expect(screen.getByRole('menuitem', { name: '字段配置' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '删除字段' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '更改类型' })).not.toBeInTheDocument()

    fireEvent.doubleClick(await screen.findByTestId('cell-rec_1-fld_title'))
    expect(await screen.findByRole('textbox')).toBeInTheDocument()
  })
})

describe('可查看（viewer）与外部账号', () => {
  it('全部只读：新增/删除/编辑/结构/配置入口隐藏，可切换已有视图', async () => {
    mockBase('viewer')
    mockTables()
    mockViews()
    mockFields()
    mockConfig()
    mockRecords([rawRecord('rec_1', 'usr_1', '记录一')])
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    expect(screen.queryByRole('button', { name: '+ 新增记录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '首行新增记录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新建数据表' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '字段操作：金额' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '筛选' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '分享' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多' })).not.toBeInTheDocument()

    fireEvent.doubleClick(await screen.findByTestId('cell-rec_1-fld_title'))
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '看板视图' }))
    expect(screen.getByRole('button', { name: '看板视图' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  it('外部账号即使是可编辑角色也不渲染分享与导出入口', async () => {
    signIn(false)
    mockBase('editor')
    mockTables()
    mockViews()
    mockFields()
    mockConfig()
    mockRecords([])
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    expect(screen.queryByRole('button', { name: '分享' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /导出/ })).not.toBeInTheDocument()
  })
})

describe('容量上限（5001 / 表 50）', () => {
  it('新建数据表返回 5001 时对话框内提示容量上限并保留输入', async () => {
    mockBase('owner')
    mockTables()
    mockViews()
    mockFields()
    mockConfig()
    mswServer.use(mockEnvelope({ path: '*/api/v1/bases/:base_id/tables', method: 'post', code: 5001 }))
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    fireEvent.click(screen.getByRole('button', { name: '新建数据表' }))
    fireEvent.change(screen.getByLabelText('数据表名称'), { target: { value: '新表' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    const dialog = screen.getByRole('dialog')
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('已达容量上限')
    expect(within(dialog).getByLabelText('数据表名称')).toHaveValue('新表')
  })

  it('Base 已有 50 张表时禁用新建并提示', async () => {
    mockBase('owner')
    const tables = Array.from({ length: 50 }, (_, index) =>
      rawTable(index === 0 ? TABLE_ID : `tbl_${index}`, index === 0 ? '项目排期' : `表 ${index + 1}`),
    )
    mockTables(tables)
    mockViews()
    mockFields()
    mockConfig()
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    expect(screen.getByRole('button', { name: '新建数据表' })).toBeDisabled()
    expect(screen.getByText('每个 Base 最多 50 张数据表')).toBeInTheDocument()
  })
})

describe('表结构入口与顶栏菜单（可管理）', () => {
  it('可管理：字段菜单全项、新建表/分享/更多全项可见', async () => {
    mockBase('owner')
    mockTables()
    mockViews()
    mockFields()
    mockConfig()
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    expect(screen.getByRole('button', { name: '新建数据表' })).toBeEnabled()
    expect(screen.getByRole('button', { name: '分享' })).toBeDisabled()

    fireEvent.click(await screen.findByRole('button', { name: '字段操作：金额' }))
    expect(screen.getByRole('menuitem', { name: '删除字段' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '更改类型' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '更多' }))
    expect(screen.getByRole('menuitem', { name: '重命名' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '复制' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '删除' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '星标' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('menuitem', { name: '星标' })).toBeInTheDocument())
  })
})
