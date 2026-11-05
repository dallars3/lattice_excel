import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'

import { lookupErrorCatalog } from '@/core/http'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { fieldKeys } from '@/query-keys'
import { workspaceStore } from '@/stores'
import { ToastProvider } from '@/ui-kit'

import type { FieldRecord } from '../api/field'
import type { TableSummary } from '../api/table'
import type { ViewSummary } from '../api/view'
import { GridArea, type GridAreaProps } from './GridArea'

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
  name: string,
  type: string,
  overrides: Partial<RawField> = {},
): RawField {
  return {
    id,
    table_id: 'tbl_1',
    name,
    type,
    config_map: {},
    is_primary: false,
    position: 0,
    description: '',
    ...overrides,
  }
}

const PRIMARY = rawField('fld_primary', '标题', 'FIELD_TYPE_TEXT', {
  is_primary: true,
  position: 0,
  description: '记录标题',
})
const STATUS = rawField('fld_status', '状态', 'FIELD_TYPE_SELECT', {
  position: 1,
  config_map: {
    option_list: [
      { id: 'opt_1', name: '进行中', color: 'blue' },
      { id: 'opt_2', name: '完成', color: 'green' },
    ],
    allow_create: false,
  },
})
const AMOUNT = rawField('fld_amount', '金额', 'FIELD_TYPE_NUMBER', {
  position: 2,
  config_map: { format: 'decimal', decimals: 2, currency: '¥', thousands: false },
})
const FORMULA = rawField('fld_formula', '合计', 'FIELD_TYPE_FORMULA', {
  position: 3,
  config_map: { expression: 'SUM({金额})' },
})
const RELATION = rawField('fld_relation', '客户', 'FIELD_TYPE_RELATION', {
  position: 4,
  config_map: { target_table_id: 'tbl_2', allow_multiple: false },
})
const LOOKUP = rawField('fld_lookup', '关联金额', 'FIELD_TYPE_LOOKUP', {
  position: 5,
  config_map: { relation_field_id: 'fld_relation', target_field_id: 'fld_amount' },
})
const DATE = rawField('fld_date', '截止日期', 'FIELD_TYPE_DATE', {
  position: 6,
  config_map: { include_time: false, format: 'YYYY-MM-DD' },
})
const PERSON = rawField('fld_person', '负责人', 'FIELD_TYPE_PERSON', {
  position: 7,
  config_map: { allow_multiple: false },
})

const BASE_FIELDS = [AMOUNT, PRIMARY, STATUS]

function normalize(raw: RawField): FieldRecord {
  return {
    id: raw.id,
    tableId: raw.table_id,
    name: raw.name,
    type: raw.type.replace('FIELD_TYPE_', '').toLowerCase() as FieldRecord['type'],
    configMap: raw.config_map,
    isPrimary: raw.is_primary,
    position: raw.position,
    description: raw.description,
  }
}

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

function fail(code: number) {
  const entry = lookupErrorCatalog(code)
  return HttpResponse.json({ code, message: entry.title, data: null }, { status: entry.httpStatus })
}

interface RenderOptions {
  props?: Partial<GridAreaProps>
  fields?: RawField[]
}

function renderGrid(options: RenderOptions = {}) {
  const fields = options.fields ?? BASE_FIELDS
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <GridArea
          tableId="tbl_1"
          fields={fields.map(normalize)}
          canManageStructure
          canEditFields
          {...options.props}
        />
      </ToastProvider>
    </QueryClientProvider>,
  )
  return { queryClient, ...view }
}

function openFieldMenu(name: string) {
  fireEvent.click(screen.getByRole('button', { name: `字段操作：${name}` }))
}

function chooseMenuItem(name: string) {
  fireEvent.click(screen.getByRole('menuitem', { name }))
}

function mockCreate(bodySpy: (body: unknown) => void, response: RawField | null = null) {
  mswServer.use(
    http.post('*/api/v1/tables/:table_id/fields', async ({ request }) => {
      bodySpy(await request.json())
      return ok({ field: response ?? rawField('fld_new', '新字段', 'FIELD_TYPE_TEXT') })
    }),
  )
}

describe('字段表头（票 04）', () => {
  it('按 position 排序、主字段固定第一列，并在表头悬停展示描述', () => {
    renderGrid()

    const headers = screen.getAllByRole('columnheader').map((node) => node.getAttribute('aria-label'))
    expect(headers).toEqual(['字段：标题', '字段：状态', '字段：金额'])

    const primaryHeader = screen.getByRole('columnheader', { name: '字段：标题' })
    expect(within(primaryHeader).getByTitle('记录标题')).toBeInTheDocument()
  })

  it('主字段菜单仅有重命名与字段配置，不提供复制/类型转换/删除', () => {
    renderGrid()
    openFieldMenu('标题')

    expect(screen.getByRole('menuitem', { name: '重命名字段' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '字段配置' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '复制字段' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '更改类型' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '删除字段' })).not.toBeInTheDocument()
  })

  it('可编辑角色不显示删除字段与更改类型，且不出现新增字段入口', () => {
    renderGrid({ props: { canManageStructure: false, canEditFields: true } })
    openFieldMenu('状态')

    expect(screen.queryByRole('menuitem', { name: '删除字段' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '更改类型' })).not.toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '复制字段' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '维护选项' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新增字段' })).not.toBeInTheDocument()
  })

  it('可填写/可查看不出现表头菜单与新增字段入口', () => {
    renderGrid({ props: { canManageStructure: false, canEditFields: false } })

    expect(screen.queryByRole('button', { name: '字段操作：状态' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新增字段' })).not.toBeInTheDocument()
  })

  it('字段列表加载失败展示错误与重试入口', () => {
    let retried = false
    renderGrid({
      props: {
        fieldsError: new Error('boom'),
        onRetryFields: () => {
          retried = true
        },
      },
    })

    expect(screen.getByRole('alert')).toHaveTextContent('操作失败，请稍后重试')
    fireEvent.click(screen.getByRole('button', { name: '重新加载字段' }))
    expect(retried).toBe(true)
  })
})

describe('新增字段（H-35）', () => {
  it('类型选择面板提供 11 种字段类型', () => {
    renderGrid()
    fireEvent.click(screen.getByRole('button', { name: '新增字段' }))

    const typeSelect = screen.getByLabelText('字段类型')
    expect(within(typeSelect).getAllByRole('option')).toHaveLength(11)
    expect(
      within(typeSelect)
        .getAllByRole('option')
        .map((option) => option.textContent),
    ).toContain('引用')
  })

  it('名称超长时行内提示且不发请求', () => {
    let posted = false
    mockCreate(() => {
      posted = true
    })
    renderGrid()
    fireEvent.click(screen.getByRole('button', { name: '新增字段' }))
    fireEvent.change(screen.getByLabelText('字段名称'), {
      target: { value: '名'.repeat(31) },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(screen.getByRole('alert')).toHaveTextContent('字段名称需为 1–30 个字符')
    expect(posted).toBe(false)
  })

  it('同表内重名时行内提示且不发请求', () => {
    let posted = false
    mockCreate(() => {
      posted = true
    })
    renderGrid()
    fireEvent.click(screen.getByRole('button', { name: '新增字段' }))
    fireEvent.change(screen.getByLabelText('字段名称'), { target: { value: '标题' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(screen.getByRole('alert')).toHaveTextContent('同表内字段名称不可重复')
    expect(posted).toBe(false)
  })

  it('新建成功：载荷不含 description/client_request_id，描述写入会话内映射', async () => {
    let body: unknown = null
    mockCreate((payload) => {
      body = payload
    })
    workspaceStore.setState({ fieldDescriptions: {} })
    renderGrid()

    fireEvent.click(screen.getByRole('button', { name: '新增字段' }))
    fireEvent.change(screen.getByLabelText('字段名称'), { target: { value: '备注' } })
    fireEvent.change(screen.getByLabelText('字段描述'), {
      target: { value: '悬停说明' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() =>
      expect(body).toEqual({ name: '备注', type: 'text', config_map: {} }),
    )
    expect(body).not.toHaveProperty('description')
    expect(body).not.toHaveProperty('client_request_id')
    expect(workspaceStore.getState().fieldDescriptions.fld_new).toBe('悬停说明')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('字段数达 100 上限时新增入口禁用并提示容量上限', () => {
    const fields = Array.from({ length: 100 }, (_, index) =>
      rawField(`fld_${index}`, `字段${index}`, 'FIELD_TYPE_TEXT', { position: index }),
    )
    renderGrid({ fields })

    const plus = screen.getByRole('button', { name: '新增字段' })
    expect(plus).toBeDisabled()
    expect(screen.getByText('字段数已达上限（100），无法新增字段')).toBeInTheDocument()
  })

  it('服务端 5001 时在对话框内提示字段容量上限并保留输入', async () => {
    mswServer.use(
      http.post('*/api/v1/tables/:table_id/fields', () => fail(5001)),
    )
    renderGrid()
    fireEvent.click(screen.getByRole('button', { name: '新增字段' }))
    fireEvent.change(screen.getByLabelText('字段名称'), { target: { value: '新字段' } })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('字段数已达上限（100）')
    expect(screen.getByLabelText('字段名称')).toHaveValue('新字段')
  })
})

describe('重命名字段（H-36）', () => {
  it('保存后请求体仅 name 并失效字段列表', async () => {
    let body: unknown = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id',
        method: 'patch',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: { ...STATUS, name: '新状态' } },
      }),
    )
    const { queryClient } = renderGrid()
    queryClient.setQueryData(fieldKeys.list('tbl_1'), [])

    openFieldMenu('状态')
    chooseMenuItem('重命名字段')
    fireEvent.change(screen.getByLabelText('字段名称'), { target: { value: '新状态' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(body).toEqual({ name: '新状态' }))
    await waitFor(() =>
      expect(queryClient.getQueryState(fieldKeys.list('tbl_1'))?.isInvalidated).toBe(true),
    )
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})

describe('字段配置面板（H-37 / H-40）', () => {
  it('数字：保存格式/小数位/货币/千分位', async () => {
    let body: unknown = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: AMOUNT },
      }),
    )
    renderGrid()

    openFieldMenu('金额')
    chooseMenuItem('字段配置')
    fireEvent.change(screen.getByLabelText('数字格式'), { target: { value: 'percent' } })
    fireEvent.change(screen.getByLabelText('货币符号'), { target: { value: '$' } })
    fireEvent.click(screen.getByLabelText('使用千分位'))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(body).toEqual({
        config_map: { format: 'percent', decimals: 2, currency: '$', thousands: true },
      }),
    )
  })

  it('日期：保存是否含时间与三种格式', async () => {
    let body: unknown = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: DATE },
      }),
    )
    renderGrid({ fields: [PRIMARY, DATE] })

    openFieldMenu('截止日期')
    chooseMenuItem('字段配置')
    fireEvent.click(screen.getByLabelText('包含时间'))
    fireEvent.change(screen.getByLabelText('日期格式'), { target: { value: 'YYYY/MM/DD' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(body).toEqual({
        config_map: { include_time: true, format: 'YYYY/MM/DD' },
      }),
    )
  })

  it('人员：保存是否多选', async () => {
    let body: unknown = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: PERSON },
      }),
    )
    renderGrid({ fields: [PRIMARY, PERSON] })

    openFieldMenu('负责人')
    chooseMenuItem('字段配置')
    fireEvent.click(screen.getByLabelText('允许选择多位人员'))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(body).toEqual({ config_map: { allow_multiple: true } }))
  })

  it('公式：保存表达式', async () => {
    let body: unknown = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: FORMULA },
      }),
    )
    renderGrid({ fields: [PRIMARY, AMOUNT, FORMULA] })

    openFieldMenu('合计')
    chooseMenuItem('字段配置')
    fireEvent.change(screen.getByLabelText('公式表达式'), {
      target: { value: 'AVG({金额})' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(body).toEqual({ config_map: { expression: 'AVG({金额})' } }))
  })

  it('关联：保存目标表与是否多条', async () => {
    let body: unknown = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: RELATION },
      }),
    )
    const tables: TableSummary[] = [
      { id: 'tbl_2', baseId: 'base_1', name: '客户档案', fieldCount: 2, viewCount: 1, createdAt: 0 },
    ]
    renderGrid({ fields: [PRIMARY, RELATION], props: { tables } })

    openFieldMenu('客户')
    chooseMenuItem('字段配置')
    fireEvent.change(screen.getByLabelText('目标数据表'), { target: { value: 'tbl_2' } })
    fireEvent.click(screen.getByLabelText('允许关联多条记录'))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(body).toEqual({
        config_map: { target_table_id: 'tbl_2', allow_multiple: true },
      }),
    )
  })

  it('引用：关联字段联动目标表字段并保存两个 id', async () => {
    let body: unknown = null
    const targetField = rawField('fld_name', '客户名', 'FIELD_TYPE_TEXT', { table_id: 'tbl_2' })
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/config',
        method: 'put',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: LOOKUP },
      }),
      http.get('*/api/v1/tables/:table_id/fields', ({ params }) =>
        ok({
          field_list: String(params.table_id) === 'tbl_2' ? [targetField] : [],
        }),
      ),
    )
    renderGrid({ fields: [PRIMARY, RELATION, LOOKUP] })

    openFieldMenu('关联金额')
    chooseMenuItem('字段配置')
    fireEvent.change(screen.getByLabelText('关联字段'), { target: { value: 'fld_relation' } })
    await waitFor(() => expect(screen.getByLabelText('目标字段')).toBeEnabled())
    fireEvent.change(screen.getByLabelText('目标字段'), { target: { value: 'fld_name' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(body).toEqual({
        config_map: { relation_field_id: 'fld_relation', target_field_id: 'fld_name' },
      }),
    )
  })

  it('文本/附件/超链接无配置项说明', () => {
    renderGrid()
    openFieldMenu('标题')
    chooseMenuItem('字段配置')

    expect(screen.getByText('该类型暂无可配置参数。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '关闭' })).toBeInTheDocument()
  })
})

describe('类型转换（H-38）', () => {
  it('只提供允许的目标类型，先预览后确认的两步载荷', async () => {
    const bodies: unknown[] = []
    mswServer.use(
      http.post('*/api/v1/fields/:field_id/convert', async ({ request }) => {
        const body = (await request.json()) as { confirm: boolean }
        bodies.push(body)
        return body.confirm
          ? ok({ field: { ...AMOUNT, type: 'FIELD_TYPE_NUMBER' }, affected_count: 3, cleared_count: 1 })
          : ok({ affected_count: 3, cleared_count: 1 })
      }),
    )
    renderGrid({ fields: [PRIMARY, STATUS] })

    openFieldMenu('状态')
    chooseMenuItem('更改类型')
    const targetSelect = screen.getByLabelText('目标类型')
    const targetLabels = within(targetSelect)
      .getAllByRole('option')
      .map((option) => option.textContent)
    expect(targetLabels).toEqual(['多选', '多行文本'])

    fireEvent.change(targetSelect, { target: { value: 'text' } })
    fireEvent.click(screen.getByRole('button', { name: '预览影响' }))
    expect(await screen.findByRole('status')).toHaveTextContent(
      '受影响记录 3 条，将清空值 1 个',
    )
    fireEvent.click(screen.getByRole('button', { name: '确认转换' }))

    await waitFor(() =>
      expect(bodies).toEqual([
        { target_type: 'text', confirm: false },
        { target_type: 'text', confirm: true },
      ]),
    )
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('公式字段不提供更改类型入口', () => {
    renderGrid({ fields: [PRIMARY, AMOUNT, FORMULA] })
    openFieldMenu('合计')
    expect(screen.queryByRole('menuitem', { name: '更改类型' })).not.toBeInTheDocument()
  })
})

describe('复制字段（H-92）', () => {
  it('复制请求无请求体', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/duplicate',
        method: 'post',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: { field: { ...STATUS, id: 'fld_copy', name: '状态 的副本' } },
      }),
    )
    renderGrid()

    openFieldMenu('状态')
    chooseMenuItem('复制字段')

    await waitFor(() => expect(rawBody).toBe(''))
  })
})

describe('删除字段（H-39）', () => {
  function mockDeletion() {
    const deleteBodies: unknown[] = []
    const savedConfigs: unknown[] = []
    mswServer.use(
      http.delete('*/api/v1/fields/:field_id', async ({ request }) => {
        deleteBodies.push(await request.json())
        return ok({ record_count: 5, dependent_formulas: 1, dependent_lookups: 1 })
      }),
      http.get('*/api/v1/views/:view_id/config', ({ params }) =>
        ok({
          view_config:
            String(params.view_id) === 'view_1'
              ? {
                  hidden_field_list: ['fld_amount'],
                  filter_list: [{ field_id: 'fld_amount', op: 'VIEW_FILTER_OP_IS_EMPTY' }],
                  field_width_map: { fld_amount: 320 },
                }
              : { sort_list: [{ field_id: 'fld_amount', dir: 'SORT_DIR_ASC' }] },
        }),
      ),
      http.put('*/api/v1/views/:view_id/config', async ({ request }) => {
        savedConfigs.push(await request.json())
        return ok({ view_config: {} })
      }),
    )
    return { deleteBodies, savedConfigs }
  }

  const views: ViewSummary[] = [
    { id: 'view_1', tableId: 'tbl_1', name: '表格视图', type: 'table', createdAt: 0 },
    { id: 'view_2', tableId: 'tbl_1', name: '客户视图', type: 'table', createdAt: 0 },
  ]

  it('展示记录数/依赖公式数/依赖引用数，确认后清理视图条件并轻提示', async () => {
    const { deleteBodies, savedConfigs } = mockDeletion()
    renderGrid({
      fields: [PRIMARY, AMOUNT, FORMULA, LOOKUP, RELATION],
      props: { views, loadedRecordCount: 5, hasMoreRecords: true },
    })

    openFieldMenu('金额')
    chooseMenuItem('删除字段')

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('影响记录：当前已加载 5 条（另有更多未加载）')).toBeInTheDocument()
    expect(within(dialog).getByText('依赖该字段的公式：1 个')).toBeInTheDocument()
    expect(within(dialog).getByText('依赖该字段的引用：1 个')).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '确认删除' }))

    await waitFor(() => expect(deleteBodies).toEqual([{ confirm: true }]))
    await waitFor(() => expect(savedConfigs).toHaveLength(2))
    const first = savedConfigs[0] as { view_config: Record<string, unknown> }
    expect(first.view_config.hidden_field_list).toEqual([])
    expect(first.view_config.filter_list).toEqual([])
    expect(first.view_config.field_width_map).toEqual({})
    const second = savedConfigs[1] as { view_config: Record<string, unknown> }
    expect(second.view_config.sort_list).toEqual([])

    await waitFor(() =>
      expect(screen.getByText('已移除 2 个视图中的失效条件')).toBeInTheDocument(),
    )
  })

  it('主字段不提供删除入口', () => {
    renderGrid()
    openFieldMenu('标题')
    expect(screen.queryByRole('menuitem', { name: '删除字段' })).not.toBeInTheDocument()
  })
})

describe('选项维护（H-40）', () => {
  it('增删改、上下排序、配色与 allow_create 开关保存为 option_list + allow_create', async () => {
    let body: unknown = null
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/fields/:field_id/options',
        method: 'put',
        onRequest: async (request) => {
          body = await request.json()
        },
        data: { field: STATUS },
      }),
    )
    renderGrid()

    openFieldMenu('状态')
    chooseMenuItem('维护选项')
    expect(screen.getByLabelText('允许录入时添加新选项')).not.toBeChecked()
    expect(screen.getByLabelText('选项名称：进行中')).toHaveValue('进行中')

    fireEvent.change(screen.getByLabelText('新选项名称'), { target: { value: '已归档' } })
    fireEvent.click(screen.getByRole('button', { name: '添加选项' }))
    expect(screen.getByLabelText('选项名称：已归档')).toBeInTheDocument()
    expect(screen.getByLabelText('选项颜色：已归档')).toHaveValue('orange')

    fireEvent.click(screen.getByRole('button', { name: '上移：已归档' }))
    fireEvent.click(screen.getByRole('button', { name: '删除选项：进行中' }))
    expect(screen.getByText('「进行中」已被记录使用，删除后对应记录值将清空。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))

    fireEvent.change(screen.getByLabelText('选项名称：完成'), { target: { value: '已完成' } })
    fireEvent.change(screen.getByLabelText('选项颜色：已归档'), { target: { value: 'red' } })
    fireEvent.click(screen.getByLabelText('允许录入时添加新选项'))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(body).toEqual({
        option_list: [
          { name: '已归档', color: 'red' },
          { id: 'opt_2', name: '已完成', color: 'green' },
        ],
        allow_create: true,
      }),
    )
  })
})
