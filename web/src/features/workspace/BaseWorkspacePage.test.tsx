import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppRoutes } from '@/app/router'
import { lookupErrorCatalog } from '@/core/http'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { viewKeys } from '@/query-keys'
import { authStore, workspaceStore } from '@/stores'

interface RawBase {
  id: string
  name: string
  table_count: number
  my_role: string
  starred: boolean
  last_modified_at: number
  last_modifier: string
}

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

function rawField(
  id: string,
  tableId: string,
  name: string,
  type = 'FIELD_TYPE_TEXT',
  overrides: Partial<RawField> = {},
): RawField {
  return {
    id,
    table_id: tableId,
    name,
    type,
    config_map: {},
    is_primary: false,
    position: 0,
    description: '',
    ...overrides,
  }
}

const BASE_ID = 'base_1'

function rawBase(overrides: Partial<RawBase> = {}): RawBase {
  return {
    id: BASE_ID,
    name: '项目排期',
    table_count: 2,
    my_role: 'owner',
    starred: false,
    last_modified_at: 1730000000000,
    last_modifier: 'Alice',
    ...overrides,
  }
}

function rawTable(id: string, name: string, baseId = BASE_ID): RawTable {
  return {
    id,
    base_id: baseId,
    name,
    field_count: 3,
    view_count: 1,
    created_at: '1730000000000',
  }
}

function rawView(id: string, tableId: string, name: string): RawView {
  return { id, table_id: tableId, name, type: 'table', created_at: '1730000000000' }
}

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

function fail(code: number) {
  const entry = lookupErrorCatalog(code)
  return HttpResponse.json(
    { code, message: entry.title, data: null },
    { status: entry.httpStatus },
  )
}

/** 工作台默认字段（票 04）：H-34 未显式 mock 时的最小主字段响应。 */
const PRIMARY_FIELD = {
  id: 'fld_primary',
  table_id: 'tbl_1',
  name: '标题',
  type: 'FIELD_TYPE_TEXT',
  config_map: {},
  is_primary: true,
  position: 0,
  description: '',
}

beforeEach(() => {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/tables/:table_id/fields',
      data: { field_list: [PRIMARY_FIELD] },
    }),
  )
})

function signIn(options: { internal?: boolean } = {}) {
  authStore.getState().clear()
  authStore.getState().setSession(
    options.internal === false
      ? { user: { id: 'usr_ext', username: 'guest' } }
      : {
          user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
          enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
        },
  )
}

function renderWorkspace(baseId = BASE_ID) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/base/${baseId}`]}>
        <AppRoutes />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return queryClient
}

function mockBase(base: RawBase) {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/home',
      data: {
        recent_list: [],
        created_list: [base],
        shared_list: [],
        star_list: [],
        invitation_list: [],
      },
    }),
  )
}

/** 表列表 GET 读取可变数组：写操作原地改数组即可让失效后的重拉看到新结构。 */
function mockTables(tables: RawTable[]) {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/tables',
      data: { table_list: tables },
    }),
  )
}

function mockViews(viewsByTable: Record<string, RawView[]>) {
  mswServer.use(
    http.get('*/api/v1/tables/:table_id/views', ({ params }) =>
      ok({ view_list: viewsByTable[String(params.table_id)] ?? [] }),
    ),
  )
}

function openBaseMenu() {
  fireEvent.click(screen.getByRole('button', { name: '更多' }))
}

function openTableMenu(name: string) {
  fireEvent.click(screen.getByRole('button', { name: `表操作：${name}` }))
}

const TABLES = [rawTable('tbl_1', '项目排期'), rawTable('tbl_2', '客户档案')]
const VIEWS: Record<string, RawView[]> = {
  tbl_1: [rawView('view_1', 'tbl_1', '表格视图')],
  tbl_2: [rawView('view_2', 'tbl_2', '客户视图')],
}

/** 每个用例独立克隆表对象：写操作会原地改名，避免跨用例污染共享 fixture。 */
function clonedTables(): RawTable[] {
  return TABLES.map((table) => ({ ...table }))
}

function expectFiveSectionShell() {
  expect(screen.getByRole('banner', { name: '工作台顶栏' })).toBeInTheDocument()
  expect(screen.getByRole('region', { name: '表与视图栏' })).toBeInTheDocument()
  expect(screen.getByRole('region', { name: '网格' })).toBeInTheDocument()
  expect(screen.getByRole('contentinfo', { name: '底栏' })).toBeInTheDocument()
}

describe('Base 工作台骨架（深链与五段式）', () => {
  beforeEach(() => {
    signIn()
    workspaceStore.setState({ inFlightWriteCount: 0, offline: false })
  })

  it('深链 /base/:id 并行加载 Base、表列表与默认视图，并呈五段式布局', async () => {
    mockBase(rawBase())
    mockTables(TABLES)
    mockViews(VIEWS)
    renderWorkspace()

    expect(
      await screen.findByRole('heading', { level: 1, name: '项目排期' }),
    ).toBeInTheDocument()
    expect(await screen.findByRole('tab', { name: '项目排期' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(
      await screen.findByRole('button', { name: '表格视图' }),
    ).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('navigation', { name: '视图' })).toBeInTheDocument()

    expectFiveSectionShell()
    expect(screen.getByText('已保存')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '分享' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '新建数据表' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '+ 新增记录' })).toBeDisabled()
    expect(screen.getByText('已加载 0 条')).toBeInTheDocument()
  })

  it('Base 不在 H-21 四列表时呈无权限态与返回首页入口，且不再请求表列表', async () => {
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/home',
        data: {
          recent_list: [],
          created_list: [],
          shared_list: [],
          star_list: [],
          invitation_list: [],
        },
      }),
    )
    renderWorkspace('base_missing')

    expect(
      await screen.findByRole('heading', { level: 1, name: '你没有访问该表格的权限' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回首页' })).toHaveAttribute('href', '/home')
  })

  it('Base 加载失败展示错误与重试入口', async () => {
    mswServer.use(
      mockEnvelope({ path: '/api/v1/home', code: 9001 }),
    )
    renderWorkspace()

    expect(await screen.findByText('服务暂时不可用')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新加载' })).toBeInTheDocument()
  })

  it('表列表加载失败展示错误与重试入口', async () => {
    mockBase(rawBase())
    mswServer.use(mockEnvelope({ path: '*/api/v1/bases/:base_id/tables', code: 9001 }))
    renderWorkspace()

    expect(await screen.findByText('服务暂时不可用')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新加载数据表' })).toBeInTheDocument()
  })
})

describe('数据表标签页与视图栏', () => {
  beforeEach(() => {
    signIn()
    workspaceStore.setState({ inFlightWriteCount: 0, offline: false })
  })

  it('展示全部数据表，切换后视图栏与当前视图跟随', async () => {
    mockBase(rawBase())
    mockTables(TABLES)
    mockViews(VIEWS)
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    fireEvent.click(screen.getByRole('tab', { name: '客户档案' }))

    await waitFor(() =>
      expect(screen.getByRole('tab', { name: '客户档案' })).toHaveAttribute(
        'aria-selected',
        'true',
      ),
    )
    expect(
      await screen.findByRole('button', { name: '客户视图' }),
    ).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('button', { name: '表格视图' })).not.toBeInTheDocument()
  })
})

describe('数据表管理（H-30 ~ H-33）', () => {
  beforeEach(() => {
    signIn()
    workspaceStore.setState({ inFlightWriteCount: 0, offline: false })
  })

  it('新建表成功（body 仅 name）并自动切换为当前表', async () => {
    mockBase(rawBase())
    const tables = clonedTables()
    mockTables(tables)
    mockViews(VIEWS)
    const createBodies: unknown[] = []
    mswServer.use(
      http.post('*/api/v1/bases/:base_id/tables', async ({ request }) => {
        createBodies.push(await request.json())
        const created = rawTable('tbl_new', '客户档案 2')
        tables.push(created)
        return ok({ table: created })
      }),
      http.get('*/api/v1/tables/:table_id/views', ({ params }) =>
        ok({
          view_list:
            String(params.table_id) === 'tbl_new'
              ? [rawView('view_new', 'tbl_new', '新表视图')]
              : VIEWS[String(params.table_id)] ?? [],
        }),
      ),
    )
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    fireEvent.click(screen.getByRole('button', { name: '新建数据表' }))
    fireEvent.change(screen.getByLabelText('数据表名称'), { target: { value: '客户档案 2' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(await screen.findByRole('tab', { name: '客户档案 2' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(createBodies).toEqual([{ name: '客户档案 2' }])
    expect(
      await screen.findByRole('button', { name: '新表视图' }),
    ).toHaveAttribute('aria-pressed', 'true')
  })

  it('新建表重名（1002）在对话框内展示冲突提示并保留输入', async () => {
    mockBase(rawBase())
    mockTables(clonedTables())
    mockViews(VIEWS)
    mswServer.use(
      http.post('*/api/v1/bases/:base_id/tables', () => fail(1002)),
    )
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    fireEvent.click(screen.getByRole('button', { name: '新建数据表' }))
    fireEvent.change(screen.getByLabelText('数据表名称'), {
      target: { value: '项目排期' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    const dialog = screen.getByRole('dialog')
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('名称已被使用')
    expect(within(dialog).getByLabelText('数据表名称')).toHaveValue('项目排期')
  })

  it('新建表名称 1–30 校验：超长时行内提示且不发请求', async () => {
    mockBase(rawBase())
    mockTables(clonedTables())
    mockViews(VIEWS)
    let posted = false
    mswServer.use(
      http.post('*/api/v1/bases/:base_id/tables', () => {
        posted = true
        return ok({ table: rawTable('tbl_x', 'x') })
      }),
    )
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    fireEvent.click(screen.getByRole('button', { name: '新建数据表' }))
    fireEvent.change(screen.getByLabelText('数据表名称'), {
      target: { value: '名称'.repeat(16) },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(within(screen.getByRole('dialog')).getByRole('alert')).toHaveTextContent(
      '名称需为 1–30 个字符',
    )
    expect(posted).toBe(false)
  })

  it('重命名表：请求体仅 name，成功后标签更新', async () => {
    mockBase(rawBase())
    const tables = clonedTables()
    mockTables(tables)
    mockViews(VIEWS)
    const bodies: unknown[] = []
    mswServer.use(
      http.patch('*/api/v1/tables/:table_id', async ({ request, params }) => {
        const body = (await request.json()) as { name: string }
        bodies.push(body)
        const target = tables.find((table) => table.id === String(params.table_id))
        if (target !== undefined) target.name = body.name
        return ok({ table: { ...(target ?? tables[0]), name: body.name } })
      }),
    )
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    openTableMenu('项目排期')
    fireEvent.click(screen.getByRole('menuitem', { name: '重命名表' }))
    fireEvent.change(screen.getByLabelText('数据表名称'), { target: { value: '新表名' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    expect(await screen.findByRole('tab', { name: '新表名' })).toBeInTheDocument()
    expect(bodies).toEqual([{ name: '新表名' }])
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('复制表：无请求体，成功后出现副本标签', async () => {
    mockBase(rawBase())
    const tables = clonedTables()
    mockTables(tables)
    mockViews(VIEWS)
    let rawBody = 'unset'
    mswServer.use(
      http.post('*/api/v1/tables/:table_id/duplicate', async ({ request }) => {
        rawBody = await request.text()
        const copy = rawTable('tbl_copy', '项目排期 副本')
        tables.push(copy)
        return ok({ table: copy })
      }),
    )
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    openTableMenu('项目排期')
    fireEvent.click(screen.getByRole('menuitem', { name: '复制表' }))

    expect(await screen.findByRole('tab', { name: '项目排期 副本' })).toBeInTheDocument()
    expect(rawBody).toBe('')
  })

  it('删除表：strong 二次确认、body confirm=true，并清理对应视图查询', async () => {
    mockBase(rawBase())
    const tables = clonedTables()
    mockTables(tables)
    mockViews(VIEWS)
    const queryClient = renderWorkspace()
    const bodies: unknown[] = []
    mswServer.use(
      http.delete('*/api/v1/tables/:table_id', async ({ request, params }) => {
        bodies.push(await request.json())
        const index = tables.findIndex((table) => table.id === String(params.table_id))
        if (index >= 0) tables.splice(index, 1)
        return ok({})
      }),
    )

    await screen.findByRole('tab', { name: '项目排期' })
    await screen.findByRole('button', { name: '表格视图' })
    openTableMenu('项目排期')
    fireEvent.click(screen.getByRole('menuitem', { name: '删除表' }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('删除后不可恢复（P0）')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))

    await waitFor(() =>
      expect(screen.queryByRole('tab', { name: '项目排期' })).not.toBeInTheDocument(),
    )
    expect(bodies).toEqual([{ confirm: true }])
    expect(
      queryClient.getQueryCache().find({ queryKey: viewKeys.list('tbl_1') }),
    ).toBeUndefined()
    await waitFor(() =>
      expect(screen.getByRole('tab', { name: '客户档案' })).toHaveAttribute(
        'aria-selected',
        'true',
      ),
    )
  })

  it('仅剩一张表时删除入口禁用', async () => {
    mockBase(rawBase())
    mockTables([rawTable('tbl_only', '唯一表')])
    mockViews({ tbl_only: [rawView('view_only', 'tbl_only', '唯一视图')] })
    renderWorkspace()

    await screen.findByRole('tab', { name: '唯一表' })
    openTableMenu('唯一表')
    expect(screen.getByRole('menuitem', { name: '删除表' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: '重命名表' })).toBeEnabled()
  })

  it('有在途写请求时当前表标签显示「保存中」，结束后恢复', async () => {
    mockBase(rawBase())
    const tables = clonedTables()
    mockTables(tables)
    mockViews(VIEWS)
    let release: () => void = () => undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    mswServer.use(
      http.patch('*/api/v1/tables/:table_id', async ({ params }) => {
        await gate
        const target = tables.find((table) => table.id === String(params.table_id))
        return ok({ table: target ?? tables[0] })
      }),
    )
    renderWorkspace()

    await screen.findByRole('tab', { name: '项目排期' })
    openTableMenu('项目排期')
    fireEvent.click(screen.getByRole('menuitem', { name: '重命名表' }))
    fireEvent.change(screen.getByLabelText('数据表名称'), { target: { value: '新表名' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(within(screen.getByRole('tab', { name: /项目排期/ })).getByText('保存中')).toBeInTheDocument()
    })
    release()
    await waitFor(() => expect(screen.queryByText('保存中')).not.toBeInTheDocument())
  })
})

describe('字段结构加载与新增（票 04）', () => {
  beforeEach(() => {
    signIn()
    workspaceStore.setState({ inFlightWriteCount: 0, offline: false, fieldDescriptions: {} })
  })

  it('按当前表加载 H-34 字段，切换表后重新加载新表字段', async () => {
    mockBase(rawBase())
    mockTables(clonedTables())
    mockViews(VIEWS)
    const requested: string[] = []
    mswServer.use(
      http.get('*/api/v1/tables/:table_id/fields', ({ params }) => {
        const tableId = String(params.table_id)
        requested.push(tableId)
        return ok({
          field_list:
            tableId === 'tbl_1'
              ? [
                  rawField('fld_title', 'tbl_1', '标题', 'FIELD_TYPE_TEXT', {
                    is_primary: true,
                    position: 0,
                    description: '记录标题',
                  }),
                ]
              : [
                  rawField('fld_customer', 'tbl_2', '客户名称', 'FIELD_TYPE_TEXT', {
                    is_primary: true,
                    position: 0,
                  }),
                  rawField('fld_level', 'tbl_2', '客户等级', 'FIELD_TYPE_SELECT', {
                    position: 1,
                    config_map: { option_list: [], allow_create: false },
                  }),
                ],
        })
      }),
    )
    renderWorkspace()

    expect(
      await screen.findByRole('columnheader', { name: '字段：标题' }),
    ).toBeInTheDocument()
    expect(screen.getByTitle('记录标题')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '客户档案' }))

    expect(
      await screen.findByRole('columnheader', { name: '字段：客户名称' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '字段：客户等级' })).toBeInTheDocument()
    expect(requested).toContain('tbl_1')
    expect(requested).toContain('tbl_2')
  })

  it('字段加载失败展示错误与重试入口', async () => {
    mockBase(rawBase())
    mockTables(clonedTables())
    mockViews(VIEWS)
    mswServer.use(mockEnvelope({ path: '*/api/v1/tables/:table_id/fields', code: 9001 }))
    renderWorkspace()

    expect(await screen.findByText('服务暂时不可用')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新加载字段' })).toBeInTheDocument()
  })

  it('从末列「＋」新增字段成功后即时出现在表头，描述写入会话内映射', async () => {
    mockBase(rawBase())
    mockTables(clonedTables())
    mockViews(VIEWS)
    const fields: RawField[] = [
      rawField('fld_title', 'tbl_1', '标题', 'FIELD_TYPE_TEXT', {
        is_primary: true,
        position: 0,
      }),
    ]
    const bodies: unknown[] = []
    mswServer.use(
      http.get('*/api/v1/tables/:table_id/fields', () =>
        ok({ field_list: fields.filter((field) => field.table_id === 'tbl_1') }),
      ),
      http.post('*/api/v1/tables/:table_id/fields', async ({ request }) => {
        const body = (await request.json()) as { name: string; description?: string }
        bodies.push(body)
        const created = rawField('fld_new', 'tbl_1', body.name, 'FIELD_TYPE_TEXT', {
          position: 1,
        })
        fields.push(created)
        return ok({ field: created })
      }),
    )
    renderWorkspace()

    await screen.findByRole('columnheader', { name: '字段：标题' })
    fireEvent.click(screen.getByRole('button', { name: '新增字段' }))
    fireEvent.change(screen.getByLabelText('字段名称'), { target: { value: '备注' } })
    fireEvent.change(screen.getByLabelText('字段描述'), { target: { value: '写在这里的说明' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(
      await screen.findByRole('columnheader', { name: '字段：备注' }),
    ).toBeInTheDocument()
    expect(bodies).toEqual([{ name: '备注', type: 'text', config_map: {} }])
    expect(screen.getByTitle('写在这里的说明')).toBeInTheDocument()
  })
})

describe('Base 顶栏「更多」菜单与权限', () => {
  beforeEach(() => {
    signIn()
    workspaceStore.setState({ inFlightWriteCount: 0, offline: false })
  })

  function mockReadyWorkspace(role: string) {
    mockBase(rawBase({ my_role: role }))
    mockTables(TABLES)
    mockViews(VIEWS)
  }

  it('可管理：重命名/复制/删除/星标全部可见，删除为强提示并回到首页', async () => {
    const home = {
      recent_list: [],
      created_list: [rawBase()],
      shared_list: [],
      star_list: [],
      invitation_list: [],
    }
    mswServer.use(mockEnvelope({ path: '/api/v1/home', data: home }))
    mockTables(clonedTables())
    mockViews(VIEWS)
    const renameBodies: unknown[] = []
    const starBodies: unknown[] = []
    mswServer.use(
      http.patch('*/api/v1/bases/:base_id', async ({ request }) => {
        const body = (await request.json()) as { name: string }
        renameBodies.push(body)
        home.created_list[0].name = body.name
        return ok({ base: rawBase({ name: body.name }) })
      }),
      http.post('*/api/v1/bases/:base_id/star', async ({ request }) => {
        starBodies.push(await request.json())
        return ok({ base: rawBase({ starred: true }) })
      }),
      http.delete('*/api/v1/bases/:base_id', () => ok({})),
    )
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    openBaseMenu()
    expect(screen.getByRole('menuitem', { name: '重命名' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '复制' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '删除' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '星标' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('menuitem', { name: '重命名' }))
    fireEvent.change(screen.getByLabelText('Base 名称'), { target: { value: '新 Base 名' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(renameBodies).toEqual([{ name: '新 Base 名' }]))
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1, name: '新 Base 名' })).toBeInTheDocument(),
    )

    openBaseMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '星标' }))
    await waitFor(() => expect(starBodies).toEqual([{ starred: true }]))

    openBaseMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '删除' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('删除后不可恢复（P0）')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1, name: '协作多维表格' })).toBeInTheDocument(),
    )
  })

  it('复制：无请求体并跳转到新 Base', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/home',
        data: {
          // 源 Base 与复制出的新 Base 都需命中 H-21 四列表，页面才能进入各自工作台。
          recent_list: [rawBase({ id: 'base_copy', name: '项目排期 副本' })],
          created_list: [rawBase({ my_role: 'manager' })],
          shared_list: [],
          star_list: [],
          invitation_list: [],
        },
      }),
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/tables',
        data: { table_list: [rawTable('tbl_copy', '项目排期 副本', 'base_copy')] },
      }),
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/views',
        data: { view_list: [rawView('view_copy', 'tbl_copy', '副本视图')] },
      }),
      http.post('*/api/v1/bases/:base_id/duplicate', async ({ request }) => {
        rawBody = await request.text()
        return ok({ base: rawBase({ id: 'base_copy', name: '项目排期 副本' }) })
      }),
    )
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    openBaseMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '复制' }))

    await waitFor(() => expect(rawBody).toBe(''))
    expect(
      await screen.findByRole('heading', { level: 1, name: '项目排期 副本' }),
    ).toBeInTheDocument()
  })

  it('viewer：菜单仅保留星标，表结构入口全部隐藏', async () => {
    mockReadyWorkspace('viewer')
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    openBaseMenu()
    expect(screen.getByRole('menuitem', { name: '星标' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '重命名' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '复制' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '删除' })).not.toBeInTheDocument()

    expect(screen.queryByRole('button', { name: '新建数据表' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '表操作：项目排期' })).not.toBeInTheDocument()
  })

  it('外部账号：顶栏不出现「更多」与表结构入口', async () => {
    signIn({ internal: false })
    mockReadyWorkspace('manager')
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: '项目排期' })
    expect(screen.queryByRole('button', { name: '更多' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新建数据表' })).not.toBeInTheDocument()
  })
})
