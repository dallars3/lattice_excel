// 视图管理与全量视图配置（票 07）：RTL + MSW，真实 protojson 形状。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppRoutes } from '@/app/router'
import { mockCursorRecords, mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore, workspaceStore } from '@/stores'

const BASE_ID = 'base_1'
const TABLE_ID = 'tbl_1'

interface RawTable {
  id: string
  base_id: string
  name: string
  field_count: number
  view_count: number
  created_at: string
}

interface RawView {
  id: string
  table_id: string
  name: string
  type: string
  created_at: string
}

interface RawField {
  id: string
  table_id: string
  name: string
  type: string
  config_map: Record<string, unknown>
  is_primary: boolean
  position: number
  description: string
}

interface RawRecord {
  id: string
  table_id: string
  cells_map: Record<string, unknown>
  created_by: string
  created_at: string
  version: string
  [key: string]: unknown
}

function rawTable(overrides: Partial<RawTable> = {}): RawTable {
  return {
    id: TABLE_ID,
    base_id: BASE_ID,
    name: '项目排期',
    field_count: 3,
    view_count: 2,
    created_at: '1730000000000',
    ...overrides,
  }
}

function rawView(id: string, name: string): RawView {
  return { id, table_id: TABLE_ID, name, type: 'table', created_at: '1730000000000' }
}

function rawField(
  id: string,
  name: string,
  type: string,
  overrides: Partial<RawField> = {},
): RawField {
  return {
    id,
    table_id: TABLE_ID,
    name,
    type: `FIELD_TYPE_${type.toUpperCase()}`,
    config_map: {},
    is_primary: false,
    position: 0,
    description: '',
    ...overrides,
  }
}

const PRIMARY = rawField('fld_primary', '标题', 'text', { is_primary: true, position: 0 })
const STATUS = rawField('fld_status', '状态', 'select', {
  position: 1,
  config_map: {
    option_list: [
      { id: 'opt_1', name: '进行中', color: 'blue' },
      { id: 'opt_2', name: '完成', color: 'green' },
    ],
    allow_create: false,
  },
})
const AMOUNT = rawField('fld_amount', '金额', 'number', {
  position: 2,
  config_map: { format: 'decimal', decimals: 2 },
})
const LOOKUP = rawField('fld_lookup', '关联金额', 'lookup', {
  position: 3,
  config_map: { relation_field_id: 'fld_amount', target_field_id: 'fld_amount' },
})
const DATE = rawField('fld_date', '截止日期', 'date', {
  position: 4,
  config_map: { include_time: false, format: 'YYYY-MM-DD' },
})

function rawRecord(id: string, cells: Record<string, unknown>): RawRecord {
  return {
    id,
    table_id: TABLE_ID,
    cells_map: cells,
    created_by: 'usr_1',
    created_at: '0',
    version: '1',
  }
}

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

function signIn() {
  authStore.getState().clear()
  authStore.getState().setSession({
    user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
    enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  })
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
  return queryClient
}

function mockBase(role = 'owner') {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/home',
      data: {
        recent_list: [],
        created_list: [
          {
            id: BASE_ID,
            name: '项目排期',
            table_count: 1,
            my_role: role,
            starred: false,
            last_modified_at: 1730000000000,
            last_modifier: 'Alice',
          },
        ],
        shared_list: [],
        star_list: [],
        invitation_list: [],
      },
    }),
  )
}

function mockTables() {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/tables',
      data: { table_list: [rawTable()] },
    }),
  )
}

function mockViewList(views: RawView[]) {
  mswServer.use(
    http.get('*/api/v1/tables/:table_id/views', () => ok({ view_list: views })),
  )
}

function mockFields(fields: RawField[] = [PRIMARY, STATUS, AMOUNT, LOOKUP, DATE]) {
  mswServer.use(
    http.get('*/api/v1/tables/:table_id/fields', () => ok({ field_list: fields })),
  )
}

function mockConfig(config: Record<string, unknown>) {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/views/:view_id/config',
      data: { view_config: config },
    }),
  )
}

function mockRecords(records: RawRecord[] = []) {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/records',
      data: { record_list: records, has_more: false, next_cursor: null },
    }),
  )
}

function mockViewConfigWrites(onWrite: (body: Record<string, unknown>) => void) {
  mswServer.use(
    http.put('*/api/v1/views/:view_id/config', async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>
      onWrite(body)
      return ok({ view_config: body.view_config })
    }),
  )
}

function lastViewConfig(body: Record<string, unknown>): Record<string, unknown> {
  return (body.view_config ?? {}) as Record<string, unknown>
}

async function waitToolbarReady() {
  const filterButton = await screen.findByRole('button', { name: '筛选' })
  await waitFor(() => expect(filterButton).toBeEnabled())
  return filterButton
}

beforeEach(() => {
  signIn()
  workspaceStore.setState({ inFlightWriteCount: 0, offline: false, fieldDescriptions: {} })
  mockBase()
  mockTables()
  mockViewList([rawView('view_1', '表格视图'), rawView('view_2', '看板视图')])
  mockFields()
  mockConfig({})
  mockRecords()
})

describe('视图配置读取（H-46）', () => {
  it('读取失败展示错误与重试入口；重试成功后工具栏可用', async () => {
    mswServer.use(
      mockEnvelope({ path: '*/api/v1/views/:view_id/config', code: 9001 }),
    )
    renderWorkspace()

    expect(await screen.findByText('服务暂时不可用')).toBeInTheDocument()
    const retry = screen.getByRole('button', { name: '重新加载视图配置' })

    mswServer.use(mockEnvelope({ path: '*/api/v1/views/:view_id/config', data: { view_config: {} } }))
    fireEvent.click(retry)

    await waitFor(() =>
      expect(screen.queryByText('服务暂时不可用')).not.toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '筛选' })).toBeEnabled())
  })
})

describe('视图 CRUD（H-42 ~ H-45）', () => {
  it('新建视图：body {name,type=table}，成功后重拉列表并自动切换', async () => {
    const views = [rawView('view_1', '表格视图')]
    mockViewList(views)
    const bodies: unknown[] = []
    mswServer.use(
      http.post('*/api/v1/tables/:table_id/views', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        bodies.push(body)
        const created = rawView('view_new', String(body.name))
        views.push(created)
        return ok({ view: created })
      }),
    )
    renderWorkspace()

    await screen.findByRole('button', { name: '表格视图' })
    fireEvent.click(screen.getByRole('button', { name: '新建视图' }))
    fireEvent.change(screen.getByLabelText('视图名称'), { target: { value: '看板清单' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(await screen.findByRole('button', { name: '看板清单' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(bodies).toEqual([{ name: '看板清单', type: 'table' }])
    expect(bodies[0]).not.toHaveProperty('client_request_id')
  })

    it('新建视图校验：1–30 字与同表重名行内错误且不发请求', async () => {
    let posted = false
    mswServer.use(
      http.post('*/api/v1/tables/:table_id/views', () => {
        posted = true
        return ok({ view: rawView('view_x', 'x') })
      }),
    )
    renderWorkspace()

    await screen.findByRole('button', { name: '表格视图' })
    fireEvent.click(screen.getByRole('button', { name: '新建视图' }))
    fireEvent.change(screen.getByLabelText('视图名称'), { target: { value: '表格视图' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByRole('alert')).toHaveTextContent('名称已被使用')
    expect(posted).toBe(false)

    fireEvent.change(within(dialog).getByLabelText('视图名称'), {
      target: { value: '名'.repeat(31) },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))
    expect(within(dialog).getByRole('alert')).toHaveTextContent('名称需为 1–30 个字符')
  })

  it('重命名视图：PATCH body {name}，成功后标签更新', async () => {
    const views = [rawView('view_1', '表格视图'), rawView('view_2', '看板视图')]
    mockViewList(views)
    const bodies: unknown[] = []
    mswServer.use(
      http.patch('*/api/v1/views/:view_id', async ({ request, params }) => {
        const body = (await request.json()) as { name: string }
        bodies.push(body)
        const target = views.find((view) => view.id === String(params.view_id))
        if (target !== undefined) target.name = body.name
        return ok({ view: target })
      }),
    )
    renderWorkspace()

    await screen.findByRole('button', { name: '表格视图' })
    fireEvent.click(screen.getByRole('button', { name: '视图操作：表格视图' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '重命名视图' }))
    fireEvent.change(screen.getByLabelText('视图名称'), { target: { value: '新视图名' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    expect(await screen.findByRole('button', { name: '新视图名' })).toBeInTheDocument()
    expect(bodies).toEqual([{ name: '新视图名' }])
  })

  it('复制视图：无请求体，成功后自动切换副本', async () => {
    const views = [rawView('view_1', '表格视图')]
    mockViewList(views)
    let rawBody = 'unset'
    mswServer.use(
      http.post('*/api/v1/views/:view_id/duplicate', async ({ request }) => {
        rawBody = await request.text()
        const copy = rawView('view_copy', '表格视图 副本')
        views.push(copy)
        return ok({ view: copy })
      }),
    )
    renderWorkspace()

    await screen.findByRole('button', { name: '表格视图' })
    fireEvent.click(screen.getByRole('button', { name: '视图操作：表格视图' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '复制视图' }))

    expect(await screen.findByRole('button', { name: '表格视图 副本' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(rawBody).toBe('')
  })

  it('删除视图：二次确认、无请求体；仅剩一个视图时删除入口禁用', async () => {
    const views = [rawView('view_1', '表格视图'), rawView('view_2', '看板视图')]
    mockViewList(views)
    let rawBody = 'unset'
    mswServer.use(
      http.delete('*/api/v1/views/:view_id', async ({ request, params }) => {
        rawBody = await request.text()
        const index = views.findIndex((view) => view.id === String(params.view_id))
        if (index >= 0) views.splice(index, 1)
        return ok({})
      }),
    )
    renderWorkspace()

    await screen.findByRole('button', { name: '表格视图' })
    fireEvent.click(screen.getByRole('button', { name: '视图操作：表格视图' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '删除视图' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))

    await waitFor(() =>
      expect(screen.queryByRole('button', { name: '表格视图' })).not.toBeInTheDocument(),
    )
    expect(rawBody).toBe('')
    expect(await screen.findByRole('button', { name: '看板视图' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    fireEvent.click(screen.getByRole('button', { name: '视图操作：看板视图' }))
    expect(screen.getByRole('menuitem', { name: '删除视图' })).toBeDisabled()
  })

  it('可查看/可填写只可切换视图：不出现新建与配置工具栏', async () => {
    mockBase('viewer')
    renderWorkspace()

    await screen.findByRole('button', { name: '表格视图' })
    fireEvent.click(screen.getByRole('button', { name: '看板视图' }))
    expect(screen.getByRole('button', { name: '看板视图' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(screen.queryByRole('button', { name: '新建视图' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '筛选' })).not.toBeInTheDocument()
  })
})

describe('筛选面板与即时保存（H-47）', () => {
  it('添加筛选、按类型白名单与 common.Value 编码；工具栏显示条件数', async () => {
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    const filterButton = await waitToolbarReady()
    fireEvent.click(filterButton)
    fireEvent.click(screen.getByRole('button', { name: '添加筛选条件' }))

    const fieldSelect = screen.getByLabelText('筛选字段 1')
    expect(within(fieldSelect).queryByRole('option', { name: '关联金额' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('筛选值 1'), { target: { value: '项目' } })
    await waitFor(() =>
      expect(lastViewConfig(writes.at(-1) ?? {}).filter_list).toEqual([
        {
          field_id: 'fld_primary',
          op: 'VIEW_FILTER_OP_CONTAINS',
          value: { string_value: '项目' },
        },
      ]),
    )
    expect(writes[0]).not.toHaveProperty('client_request_id')
    expect(within(filterButton).getByText('1')).toBeInTheDocument()

    fireEvent.change(fieldSelect, { target: { value: 'fld_status' } })
    fireEvent.click(screen.getByLabelText('筛选选项 1：进行中'))
    await waitFor(() =>
      expect(lastViewConfig(writes.at(-1) ?? {}).filter_list).toEqual([
        {
          field_id: 'fld_status',
          op: 'VIEW_FILTER_OP_IN',
          value: {
            list_value: {
              value_list: [{ string_value: 'opt_1' }],
            },
          },
        },
      ]),
    )
  })

  it('新增未填值条件不写回、不重拉、不使网格变空；补全后才保存并重拉，徽标只计完整条件', async () => {
    const rows = [
      rawRecord('rec_1', { fld_primary: { string_value: '需求一' } }),
    ]
    const recordRequests: URL[] = []
    mswServer.use(
      mockCursorRecords({
        path: '*/api/v1/bases/:base_id/records',
        rows,
        onRequest: (request) => recordRequests.push(new URL(request.url)),
      }),
    )
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    await screen.findByTestId('cell-rec_1-fld_primary')
    const filterButton = await waitToolbarReady()
    const requestsBefore = recordRequests.length

    fireEvent.click(filterButton)
    fireEvent.click(screen.getByRole('button', { name: '添加筛选条件' }))

    // 未填值：面板提示行内可见，但不下发 H-47、不产生新查询 key、网格记录仍在。
    expect(screen.getByText('请补全筛选值后生效')).toBeInTheDocument()
    expect(within(filterButton).queryByText('1')).not.toBeInTheDocument()
    expect(writes).toHaveLength(0)
    expect(recordRequests.length).toBe(requestsBefore)
    expect(screen.getByTestId('cell-rec_1-fld_primary')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('筛选值 1'), { target: { value: '需求' } })

    await waitFor(() =>
      expect(lastViewConfig(writes.at(-1) ?? {}).filter_list).toEqual([
        {
          field_id: 'fld_primary',
          op: 'VIEW_FILTER_OP_CONTAINS',
          value: { string_value: '需求' },
        },
      ]),
    )
    await waitFor(() => expect(recordRequests.length).toBeGreaterThan(requestsBefore))
    expect(within(filterButton).getByText('1')).toBeInTheDocument()
    expect(screen.queryByText('请补全筛选值后生效')).not.toBeInTheDocument()
  })

  it('第 6 条筛选被拒并提示上限', async () => {
    mockConfig({
      filter_list: Array.from({ length: 5 }, (_, index) => ({
        field_id: 'fld_primary',
        op: 'VIEW_FILTER_OP_CONTAINS',
        value: { string_value: `x${index}` },
      })),
    })
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '添加筛选条件' }))

    expect(screen.getByText('筛选条件最多 5 条')).toBeInTheDocument()
    expect(writes).toHaveLength(0)
  })

  it('筛选无结果空态与「清除筛选」写回空筛选', async () => {
    mockConfig({
      filter_list: [
        { field_id: 'fld_primary', op: 'VIEW_FILTER_OP_CONTAINS', value: { string_value: '无' } },
      ],
    })
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    expect(await screen.findByText('没有符合筛选条件的记录')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '清除筛选' }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(lastViewConfig(writes[0]).filter_list).toEqual([])
  })
})

describe('排序面板（≤3 级）', () => {
  it('第 4 级被拒并提示；升降序写回 SORT_DIR_*', async () => {
    mockConfig({
      sort_list: [
        { field_id: 'fld_primary', dir: 'SORT_DIR_ASC' },
        { field_id: 'fld_status', dir: 'SORT_DIR_ASC' },
        { field_id: 'fld_amount', dir: 'SORT_DIR_ASC' },
      ],
    })
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '排序' }))
    fireEvent.click(screen.getByRole('button', { name: '添加排序' }))
    expect(screen.getByText('排序最多 3 级')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('排序方向 1'), { target: { value: 'desc' } })
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(lastViewConfig(writes[0]).sort_list).toEqual([
      { field_id: 'fld_primary', dir: 'SORT_DIR_DESC' },
      { field_id: 'fld_status', dir: 'SORT_DIR_ASC' },
      { field_id: 'fld_amount', dir: 'SORT_DIR_ASC' },
    ])
  })
})

describe('分组面板（单字段 + 折叠，N1 组头无计数）', () => {
  it('组头仅组名与折叠控件；折叠隐藏该组数据行', async () => {
    mockConfig({ group_by_list: [{ field_id: 'fld_status' }] })
    mockRecords([
      rawRecord('rec_1', { fld_primary: { string_value: '需求一' }, fld_status: { string_value: 'opt_1' } }),
      rawRecord('rec_2', { fld_primary: { string_value: '需求二' }, fld_status: { string_value: 'opt_1' } }),
      rawRecord('rec_3', { fld_primary: { string_value: '需求三' }, fld_status: { string_value: 'opt_2' } }),
    ])
    renderWorkspace()

    const groupHeader = await screen.findByRole('button', { name: '分组：进行中' })
    expect(groupHeader).not.toHaveTextContent('2')
    expect(await screen.findByTestId('cell-rec_1-fld_primary')).toBeInTheDocument()
    expect(await screen.findByTestId('cell-rec_2-fld_primary')).toBeInTheDocument()

    fireEvent.click(groupHeader)
    await waitFor(() => {
      expect(screen.queryByTestId('cell-rec_1-fld_primary')).not.toBeInTheDocument()
      expect(screen.queryByTestId('cell-rec_2-fld_primary')).not.toBeInTheDocument()
    })
    expect(screen.getByTestId('cell-rec_3-fld_primary')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '分组：完成' })).toBeInTheDocument()
  })

  it('日期分组可选日/周/月粒度并写回 GRANULARITY_*', async () => {
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '分组' }))
    fireEvent.change(screen.getByLabelText('分组字段'), { target: { value: 'fld_date' } })
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(lastViewConfig(writes[0]).group_by_list).toEqual([
      { field_id: 'fld_date', granularity: 'GRANULARITY_DAY' },
    ])

    fireEvent.change(screen.getByLabelText('日期粒度'), { target: { value: 'month' } })
    await waitFor(() =>
      expect(lastViewConfig(writes.at(-1) ?? {}).group_by_list).toEqual([
        { field_id: 'fld_date', granularity: 'GRANULARITY_MONTH' },
      ]),
    )
  })
})

describe('显隐 / 冻结 / 行高 / 列宽拖拽', () => {
  it('隐藏字段从网格移除；恢复全部显示', async () => {
    mockConfig({ hidden_field_list: ['fld_status'] })
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    await screen.findByRole('columnheader', { name: '字段：标题' })
    await waitToolbarReady()
    expect(screen.queryByRole('columnheader', { name: '字段：状态' })).not.toBeInTheDocument()

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '字段显隐' }))
    fireEvent.click(screen.getByLabelText('显示字段：金额'))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(lastViewConfig(writes[0]).hidden_field_list).toEqual(['fld_status', 'fld_amount'])

    fireEvent.click(screen.getByRole('button', { name: '恢复全部显示' }))
    await waitFor(() =>
      expect(lastViewConfig(writes.at(-1) ?? {}).hidden_field_list).toEqual([]),
    )
    expect(await screen.findByRole('columnheader', { name: '字段：状态' })).toBeInTheDocument()
  })

  it('三档行高与冻结到第 N 列写回枚举与字段前缀', async () => {
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '字段显隐' }))
    fireEvent.click(screen.getByRole('button', { name: '紧凑' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(lastViewConfig(writes[0]).row_height).toBe('ROW_HEIGHT_COMPACT')

    fireEvent.change(screen.getByLabelText('冻结到第 N 列'), { target: { value: '2' } })
    await waitFor(() =>
      expect(lastViewConfig(writes.at(-1) ?? {}).frozen_field_list).toEqual([
        'fld_primary',
        'fld_status',
      ]),
    )
  })

  it('列宽拖拽 clamp 40–2000 并写入 field_width_map', async () => {
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    await screen.findByRole('columnheader', { name: '字段：标题' })
    await waitToolbarReady()
    const handle = screen.getByRole('separator', { name: '拖拽调整列宽：标题' })
    fireEvent.mouseDown(handle, { clientX: 100 })
    fireEvent.mouseMove(window, { clientX: 300 })
    fireEvent.mouseUp(window)

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(lastViewConfig(writes[0]).field_width_map).toEqual({ fld_primary: 360 })
  })
})

describe('空态与游标重置', () => {
  it('全部字段隐藏：提示并可恢复显隐', async () => {
    mockConfig({ hidden_field_list: ['fld_primary', 'fld_status', 'fld_amount', 'fld_lookup', 'fld_date'] })
    const writes: Record<string, unknown>[] = []
    mockViewConfigWrites((body) => writes.push(body))
    renderWorkspace()

    expect(await screen.findByText('所有字段均已隐藏')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '恢复字段显示' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(lastViewConfig(writes[0]).hidden_field_list).toEqual([])
    expect(await screen.findByRole('columnheader', { name: '字段：标题' })).toBeInTheDocument()
  })

  it('筛选变更产生新查询 key，从空游标重拉（不拼接旧页）', async () => {
    const rows = Array.from({ length: 25 }, (_, index) =>
      rawRecord(`rec_${index + 1}`, {
        fld_primary: { string_value: `记录 ${index + 1}` },
      }),
    )
    const requests: URL[] = []
    mswServer.use(
      mockCursorRecords({
        path: '*/api/v1/bases/:base_id/records',
        rows,
        onRequest: (request) => requests.push(new URL(request.url)),
      }),
      http.put('*/api/v1/views/:view_id/config', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        return ok({ view_config: body.view_config })
      }),
    )
    renderWorkspace()

    await screen.findByText('记录 20')
    await new Promise((resolve) => setTimeout(resolve, 50))
    const viewport = screen.getByRole('grid', { name: '记录网格' })
    viewport.scrollTop = 400
    fireEvent.scroll(viewport)
    await waitFor(() =>
      expect(requests.some((url) => url.searchParams.get('cursor') === 'cur_20')).toBe(true),
    )

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '添加筛选条件' }))
    fireEvent.change(screen.getByLabelText('筛选值 1'), { target: { value: '记录' } })

    await waitFor(() => expect(requests.length).toBeGreaterThan(2))
    const last = requests.at(-1)
    expect(last?.searchParams.get('cursor')).toBeNull()
  })
})

describe('即时保存合并与失败重试', () => {
  it('连续改动合并：in-flight 完成后只提交最新一份', async () => {
    let release: () => void = () => undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const bodies: Record<string, unknown>[] = []
    let puts = 0
    mswServer.use(
      http.put('*/api/v1/views/:view_id/config', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        bodies.push(body)
        puts += 1
        if (puts === 1) await gate
        return ok({ view_config: body.view_config })
      }),
    )
    renderWorkspace()

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '字段显隐' }))
    fireEvent.click(screen.getByRole('button', { name: '紧凑' }))
    await waitFor(() => expect(puts).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: '宽松' }))
    release()

    await waitFor(() => expect(puts).toBe(2))
    expect(lastViewConfig(bodies[1]).row_height).toBe('ROW_HEIGHT_TALL')
    expect(screen.getByRole('button', { name: '宽松' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('保存失败保留本地改动并显示错误；重试成功后恢复', async () => {
    const bodies: Record<string, unknown>[] = []
    mswServer.use(
      mockEnvelope({ path: '*/api/v1/views/:view_id/config', method: 'put', code: 9001 }),
    )
    renderWorkspace()

    fireEvent.click(await waitToolbarReady())
    fireEvent.click(screen.getByRole('button', { name: '字段显隐' }))
    fireEvent.click(screen.getByRole('button', { name: '紧凑' }))

    expect(await screen.findByText(/服务暂时不可用/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '紧凑' })).toHaveAttribute('aria-pressed', 'true')

    mswServer.use(
      http.put('*/api/v1/views/:view_id/config', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        bodies.push(body)
        return ok({ view_config: body.view_config })
      }),
    )
    fireEvent.click(screen.getByRole('button', { name: '重新加载视图配置' }))

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(lastViewConfig(bodies[0]).row_height).toBe('ROW_HEIGHT_COMPACT')
    await waitFor(() =>
      expect(screen.queryByText(/服务暂时不可用/)).not.toBeInTheDocument(),
    )
  })
})
