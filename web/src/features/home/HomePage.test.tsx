import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore } from '@/stores'

const T1 = new Date(2026, 10, 5, 10, 30).getTime()

interface RawBase {
  id: string
  name: string
  table_count: number
  my_role: string
  starred: boolean
  last_modified_at: number | string
  last_modifier: string
}

interface RawHome {
  recent_list: RawBase[]
  created_list: RawBase[]
  shared_list: RawBase[]
  star_list: RawBase[]
  invitation_list: unknown[]
}

function rawBase(overrides: Partial<RawBase> & Pick<RawBase, 'id' | 'name'>): RawBase {
  return {
    table_count: 1,
    my_role: 'owner',
    starred: false,
    last_modified_at: T1,
    last_modifier: 'Alice',
    ...overrides,
  }
}

function rawHome(overrides: Partial<RawHome> = {}): RawHome {
  return {
    recent_list: [],
    created_list: [],
    shared_list: [],
    star_list: [],
    invitation_list: [],
    ...overrides,
  }
}

function signIn(options: { internal: boolean } = { internal: true }) {
  authStore.getState().clear()
  authStore.getState().setSession(
    options.internal
      ? {
          user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
          enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
        }
      : { user: { id: 'usr_ext', username: 'guest' } },
  )
}

function renderHome() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/home']}>
        <AppRoutes />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function mockHome(home: RawHome) {
  mswServer.use(mockEnvelope({ path: '/api/v1/home', data: home }))
}

/** 进入工作台后的结构域最小响应（票 03：H-29 表列表 + H-41 视图列表）。 */
function mockWorkspaceDomain() {
  mswServer.use(
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/tables',
      data: {
        table_list: [
          {
            id: 'tbl_1',
            base_id: 'base_open',
            name: '数据表 1',
            field_count: 1,
            view_count: 1,
            created_at: '1730000000000',
          },
        ],
      },
    }),
    mockEnvelope({
      path: '*/api/v1/tables/:table_id/views',
      data: {
        view_list: [
          {
            id: 'view_1',
            table_id: 'tbl_1',
            name: '表格视图',
            type: 'table',
            created_at: '1730000000000',
          },
        ],
      },
    }),
    mockEnvelope({
      path: '*/api/v1/views/:view_id/config',
      data: { view_config: {} },
    }),
    mockEnvelope({
      path: '*/api/v1/tables/:table_id/fields',
      data: {
        field_list: [
          {
            id: 'fld_primary',
            table_id: 'tbl_1',
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
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/records',
      data: { record_list: [], has_more: false, next_cursor: null },
    }),
  )
}

const FULL_HOME = rawHome({
  star_list: [rawBase({ id: 'base_star', name: '客户跟进', starred: true, table_count: 4, last_modifier: 'Carol' })],
  recent_list: [rawBase({ id: 'base_recent', name: '设计素材库', table_count: 3 })],
  created_list: [rawBase({ id: 'base_created', name: '项目排期', table_count: 2 })],
  shared_list: [rawBase({ id: 'base_shared', name: '团队周报', my_role: 'editor' })],
})

describe('首页聚合（H-21）', () => {
  beforeEach(() => {
    signIn()
  })

  it('四区按「星标 → 最近访问 → 我创建的 → 与我共享」渲染；卡片含名称/表数/本地化修改时间/修改人/星标', async () => {
    mockHome(FULL_HOME)
    renderHome()

    expect(await screen.findByRole('article', { name: '多维表格：客户跟进' })).toBeInTheDocument()
    const headings = screen.getAllByRole('heading', { level: 2 }).map((node) => node.textContent)
    expect(headings).toEqual(['星标', '最近访问', '我创建的', '与我共享'])

    const starred = screen.getByRole('article', { name: '多维表格：客户跟进' })
    expect(within(starred).getByText('4 个数据表')).toBeInTheDocument()
    expect(within(starred).getByText('最近修改：2026/11/05 10:30')).toBeInTheDocument()
    expect(within(starred).getByText('Carol')).toBeInTheDocument()
    expect(within(starred).getByLabelText('已星标')).toBeInTheDocument()

    const recent = screen.getByRole('article', { name: '多维表格：设计素材库' })
    expect(within(recent).getByText('3 个数据表')).toBeInTheDocument()
    expect(within(recent).getByText('Alice')).toBeInTheDocument()
    expect(within(recent).queryByLabelText('已星标')).not.toBeInTheDocument()
  })

  it('「与我共享为空」展示克制空态（无额外入口按钮）', async () => {
    mockHome(rawHome())
    renderHome()

    expect(await screen.findByText('暂无与你共享的多维表格')).toBeInTheDocument()
    expect(
      screen.getByText('他人将多维表格共享给你后，会出现在这里。'),
    ).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: '与我共享' })).queryByRole('button')).toBeNull()
  })

  it('点击卡片名称进入该 Base 工作台', async () => {
    mockHome(rawHome({ recent_list: [rawBase({ id: 'base_open', name: '打开目标' })] }))
    mockWorkspaceDomain()
    renderHome()

    fireEvent.click(await screen.findByRole('button', { name: '打开多维表格：打开目标' }))
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('打开目标'),
    )
    expect(await screen.findByRole('tab', { name: '数据表 1' })).toBeInTheDocument()
  })
})

describe('首页搜索（H-22）', () => {
  beforeEach(() => {
    signIn()
  })

  it('输入 1–50 字发起 q 搜索并展示结果', async () => {
    const home = rawHome()
    mockHome(home)
    let query = ''
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/bases',
        onRequest: (request) => {
          query = new URL(request.url).searchParams.get('q') ?? ''
        },
        data: {
          base_list: [
            rawBase({
              id: 'base_hit',
              name: '设计规范',
              last_modified_at: '1730000000000',
              my_role: 'viewer',
            }),
          ],
        },
      }),
    )
    renderHome()

    fireEvent.change(screen.getByLabelText('搜索多维表格名称'), { target: { value: '设计' } })

    expect(await screen.findByRole('article', { name: '多维表格：设计规范' })).toBeInTheDocument()
    expect(query).toBe('设计')
    expect(screen.queryByRole('heading', { level: 2, name: '星标' })).not.toBeInTheDocument()
  })

  it('无结果时展示克制文案', async () => {
    mockHome(rawHome())
    mswServer.use(mockEnvelope({ path: '/api/v1/bases', data: { base_list: [] } }))
    renderHome()

    fireEvent.change(screen.getByLabelText('搜索多维表格名称'), {
      target: { value: '不存在' },
    })

    expect(await screen.findByText('没有找到匹配「不存在」的多维表格')).toBeInTheDocument()
  })
})

describe('首页新建 Base（H-23）', () => {
  beforeEach(() => {
    signIn()
  })

  it('空名使用默认名称，成功后直接进入该 Base 工作台', async () => {
    const home = rawHome()
    mockHome(home)
    mockWorkspaceDomain()
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/bases',
        method: 'post',
        onRequest: async (request) => {
          // H-21 聚合在真实后端已包含新建 Base；测试同步补入四列表供工作台深链查找。
          home.created_list.push(rawBase({ id: 'base_new', name: '未命名多维表格' }))
          body = (await request.json()) as Record<string, unknown>
        },
        data: { base: rawBase({ id: 'base_new', name: '未命名多维表格' }) },
      }),
    )
    renderHome()

    fireEvent.click(await screen.findByRole('button', { name: '新建多维表格' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('未命名多维表格'),
    )
    expect(body).toEqual({ name: '未命名多维表格' })
  })

  it('5001 容量上限：提示错误并停留在首页', async () => {
    mockHome(rawHome())
    mswServer.use(mockEnvelope({ path: '/api/v1/bases', method: 'post', code: 5001 }))
    renderHome()

    fireEvent.click(await screen.findByRole('button', { name: '新建多维表格' }))

    expect(await screen.findByText('已达容量上限')).toBeInTheDocument()
    expect(screen.getByText('请清理不需要的数据后重试，或联系管理员')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格')
  })
})

describe('首页卡片管理（H-24 ~ H-27）', () => {
  beforeEach(() => {
    signIn()
  })

  function openMenu(name: string) {
    fireEvent.click(screen.getByRole('button', { name: `更多操作：${name}` }))
  }

  it('重命名：调用 H-24 并刷新卡片名称', async () => {
    const home = rawHome({
      created_list: [rawBase({ id: 'base_rename', name: '设计素材库' })],
    })
    mockHome(home)
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id',
        method: 'patch',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
          home.created_list[0].name = '新名称'
        },
        data: { base: rawBase({ id: 'base_rename', name: '新名称' }) },
      }),
    )
    renderHome()

    await screen.findByRole('article', { name: '多维表格：设计素材库' })
    openMenu('设计素材库')
    fireEvent.click(screen.getByRole('menuitem', { name: '重命名' }))

    const input = screen.getByLabelText('Base 名称')
    expect(input).toHaveValue('设计素材库')
    fireEvent.change(input, { target: { value: '新名称' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(body).toEqual({ name: '新名称' }))
    expect(
      await screen.findByRole('button', { name: '打开多维表格：新名称' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('重命名失败（1001）保留输入并展示错误文案', async () => {
    const home = rawHome({
      created_list: [rawBase({ id: 'base_rename', name: '设计素材库' })],
    })
    mockHome(home)
    mswServer.use(
      mockEnvelope({ path: '*/api/v1/bases/:base_id', method: 'patch', code: 1001 }),
    )
    renderHome()

    await screen.findByRole('article', { name: '多维表格：设计素材库' })
    openMenu('设计素材库')
    fireEvent.click(screen.getByRole('menuitem', { name: '重命名' }))
    fireEvent.change(screen.getByLabelText('Base 名称'), { target: { value: '新名称' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    expect(await screen.findByText(/请求参数缺失或不合法/)).toBeInTheDocument()
    expect(screen.getByLabelText('Base 名称')).toHaveValue('新名称')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('复制：调用 H-25 且不发送请求体', async () => {
    const home = rawHome({
      created_list: [rawBase({ id: 'base_copy_src', name: '设计素材库' })],
    })
    mockHome(home)
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/duplicate',
        method: 'post',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: { base: rawBase({ id: 'base_copy_new', name: '设计素材库 副本' }) },
      }),
    )
    renderHome()

    await screen.findByRole('article', { name: '多维表格：设计素材库' })
    openMenu('设计素材库')
    fireEvent.click(screen.getByRole('menuitem', { name: '复制' }))

    await waitFor(() => expect(rawBody).toBe(''))
  })

  it('删除：展示「删除后不可恢复（P0）」二次确认并调用 H-26', async () => {
    const home = rawHome({
      created_list: [rawBase({ id: 'base_delete', name: '设计素材库' })],
    })
    mockHome(home)
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id',
        method: 'delete',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
          home.created_list = home.created_list.filter((item) => item.id !== 'base_delete')
        },
        data: {},
      }),
    )
    renderHome()

    await screen.findByRole('article', { name: '多维表格：设计素材库' })
    openMenu('设计素材库')
    fireEvent.click(screen.getByRole('menuitem', { name: '删除' }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('删除后不可恢复（P0）')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))

    await waitFor(() => expect(body).toEqual({ confirm: true }))
    await waitFor(() =>
      expect(screen.queryByRole('article', { name: '多维表格：设计素材库' })).not.toBeInTheDocument(),
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('星标切换：调用 H-27 并更新星标状态', async () => {
    const home = rawHome({
      star_list: [
        rawBase({ id: 'base_star_toggle', name: '客户跟进', starred: true, last_modifier: 'Carol' }),
      ],
    })
    mockHome(home)
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/star',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
          home.star_list = home.star_list.filter((item) => item.id !== 'base_star_toggle')
        },
        data: { base: rawBase({ id: 'base_star_toggle', name: '客户跟进', starred: false }) },
      }),
    )
    renderHome()

    const card = await screen.findByRole('article', { name: '多维表格：客户跟进' })
    expect(within(card).getByLabelText('已星标')).toBeInTheDocument()
    openMenu('客户跟进')
    fireEvent.click(screen.getByRole('menuitem', { name: '取消星标' }))

    await waitFor(() => expect(body).toEqual({ starred: false }))
    await waitFor(() =>
      expect(
        screen.queryByRole('article', { name: '多维表格：客户跟进' }),
      ).not.toBeInTheDocument(),
    )
  })

  it('viewer 角色菜单仅保留星标项（写入口隐藏）', async () => {
    mockHome(
      rawHome({
        shared_list: [rawBase({ id: 'base_viewer', name: '只读表格', my_role: 'viewer' })],
      }),
    )
    renderHome()

    await screen.findByRole('article', { name: '多维表格：只读表格' })
    openMenu('只读表格')
    expect(screen.getByRole('menuitem', { name: '星标' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '重命名' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '删除' })).not.toBeInTheDocument()
  })
})

describe('外部账号首页', () => {
  beforeEach(() => {
    signIn({ internal: false })
  })

  it('仅展示「与我共享」，隐藏新建入口与所有卡片管理菜单', async () => {
    mockHome(
      rawHome({
        shared_list: [rawBase({ id: 'base_ext', name: '外部可看', my_role: 'manager' })],
        recent_list: [rawBase({ id: 'base_recent', name: '不应出现' })],
      }),
    )
    renderHome()

    expect(await screen.findByRole('article', { name: '多维表格：外部可看' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新建多维表格' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/更多操作/)).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2, name: '星标' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2, name: '最近访问' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2, name: '我创建的' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: '与我共享' })).toBeInTheDocument()
    expect(screen.queryByRole('article', { name: '多维表格：不应出现' })).not.toBeInTheDocument()
  })
})
