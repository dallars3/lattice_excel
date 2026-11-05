// 记录详情抽屉组件测试（票 08）：RTL + MSW，真实 H-48/H-49/H-51/H-77/H-80/H-81 形状。
// 覆盖：全字段渲染（含隐藏字段、公式/引用只读）、主字段与行内编辑提交、失败保留输入、
// 4001/4002 冲突、上一条/下一条（含补拉）、附件上传/下载/重命名/删除与三项预检。
import '@testing-library/jest-dom/vitest'

import { useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { mswServer } from '@/mocks/server'
import { workspaceStore } from '@/stores'

import type { FieldRecord } from '@/features/workspace/api/field'

import { RecordDrawer } from './RecordDrawer'

interface RawRow {
  id: string
  table_id: string
  cells_map: Record<string, unknown>
  created_by: string
  created_at: string
  version: string
}

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

function fail(code: number, data: unknown = null) {
  return HttpResponse.json({ code, message: `错误 ${code}`, data }, { status: code === 4001 ? 409 : 409 })
}

function str(value: string) {
  return { string_value: value }
}

function num(value: number) {
  return { double_value: value }
}

function int(value: number | string) {
  return { int_value: String(value) }
}

function ts(value: number) {
  return { date_value_ms: String(value) }
}

function list(...values: unknown[]) {
  return { list_value: { value_list: values } }
}

function makeField(
  id: string,
  name: string,
  type: FieldRecord['type'],
  position: number,
  overrides: Partial<FieldRecord> = {},
): FieldRecord {
  return {
    id,
    tableId: 'tbl_1',
    name,
    type,
    configMap: {},
    isPrimary: false,
    position,
    description: '',
    ...overrides,
  }
}

function makeRow(
  id: string,
  cellsMap: Record<string, unknown>,
  overrides: Partial<RawRow> = {},
): RawRow {
  return {
    id,
    table_id: 'tbl_1',
    cells_map: cellsMap,
    created_by: 'usr_1',
    created_at: '0',
    version: '1',
    ...overrides,
  }
}

interface InstallOptions {
  rows: RawRow[]
  fields?: FieldRecord[]
  targetRows?: RawRow[]
  pageLimit?: number
}

/** 安装抽屉所需的全部后端路由（记录分页、H-49、元数据、目标表）。 */
function installHandlers({ rows, fields = [], targetRows = [] }: InstallOptions, pageLimit = 100) {
  mswServer.use(
    http.get('*/api/v1/bases/:base_id/records', ({ request }) => {
      const url = new URL(request.url)
      if (url.searchParams.get('table_id') === 'tbl_2') {
        return ok({ record_list: targetRows, has_more: false, next_cursor: null })
      }
      const cursor = url.searchParams.get('cursor') ?? ''
      const offset = Number.parseInt(cursor.replace(/^cur_/, ''), 10) || 0
      const limit = Math.min(
        Number.parseInt(url.searchParams.get('limit') ?? '20', 10) || 20,
        pageLimit,
      )
      const slice = rows.slice(offset, offset + limit)
      const hasMore = offset + limit < rows.length
      return ok({
        record_list: slice,
        has_more: hasMore,
        next_cursor: hasMore ? `cur_${offset + limit}` : null,
      })
    }),
    http.get('*/api/v1/records/:record_id', ({ params }) => {
      const row = rows.find((candidate) => candidate.id === String(params.record_id))
      if (row === undefined) return fail(4002)
      return ok({ record: row })
    }),
    http.get('*/api/v1/bases/:base_id/shares/members', () =>
      ok({ collaborator_list: [{ user_id: 'usr_1', display_name: '张三' }] }),
    ),
    http.get('*/api/v1/tables/:table_id/fields', ({ params }) => {
      const tableId = String(params.table_id)
      if (tableId === 'tbl_2') {
        return ok({
          field_list: [
            {
              id: 'fld_customer',
              table_id: 'tbl_2',
              name: '客户名',
              type: 'FIELD_TYPE_TEXT',
              config_map: {},
              is_primary: true,
              position: 0,
              description: '',
            },
          ],
        })
      }
      return ok({
        field_list: fields.map((field) => ({
          id: field.id,
          table_id: field.tableId,
          name: field.name,
          type: `FIELD_TYPE_${field.type.toUpperCase()}`,
          config_map: field.configMap,
          is_primary: field.isPrimary,
          position: field.position,
          description: field.description,
        })),
      })
    }),
    http.get('*/api/v1/tables/:table_id/views', () =>
      ok({
        view_list: [
          { id: 'view_target', table_id: 'tbl_2', name: '默认', type: 'table', created_at: '0' },
        ],
      }),
    ),
  )
}

interface HarnessOptions extends InstallOptions {
  initialRecordId?: string
  canEditCell?: boolean
  editOwnOnly?: boolean
  currentUserId?: string
  clipboard?: { writeText: (text: string) => Promise<void>; readText: () => Promise<string> }
}

function Harness(props: HarnessOptions) {
  const [recordId, setRecordId] = useState(props.initialRecordId ?? props.rows[0]?.id ?? '')
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return (
    <QueryClientProvider client={queryClient}>
      <RecordDrawer
        baseId="base_1"
        tableId="tbl_1"
        viewId="view_1"
        recordId={recordId}
        fields={props.fields ?? []}
        canEditCell={props.canEditCell ?? true}
        editOwnOnly={props.editOwnOnly}
        currentUserId={props.currentUserId}
        clipboard={props.clipboard}
        onClose={() => undefined}
        onNavigateRecord={setRecordId}
      />
    </QueryClientProvider>
  )
}

function renderDrawer(options: HarnessOptions) {
  installHandlers(options)
  const writeText = vi.fn(async () => undefined)
  const readText = vi.fn(async () => '')
  const clipboard = options.clipboard ?? { writeText, readText }
  render(<Harness {...options} clipboard={clipboard} />)
  return { writeText, readText }
}

const ALL_FIELDS: FieldRecord[] = [
  makeField('fld_title', '标题', 'text', 0, { isPrimary: true }),
  makeField('fld_num', '金额', 'number', 1, {
    configMap: { format: 'decimal', decimals: 2 },
  }),
  makeField('fld_status', '状态', 'select', 2, {
    configMap: {
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
      allow_create: false,
    },
  }),
  makeField('fld_tags', '标签', 'multiselect', 3, {
    configMap: {
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
      allow_create: false,
    },
  }),
  makeField('fld_date', '日期', 'date', 4, {
    configMap: { include_time: false, format: 'YYYY-MM-DD' },
  }),
  makeField('fld_person', '负责人', 'person', 5, { configMap: { allow_multiple: false } }),
  makeField('fld_attach', '附件', 'attachment', 6, { configMap: {} }),
  makeField('fld_url', '官网', 'url', 7, { configMap: {} }),
  makeField('fld_formula', '合计', 'formula', 8, { configMap: { expression: 'SUM' } }),
  makeField('fld_relation', '关联客户', 'relation', 9, {
    configMap: { target_table_id: 'tbl_2', allow_multiple: false },
  }),
  makeField('fld_lookup', '客户等级', 'lookup', 10, {
    configMap: { relation_field_id: 'fld_relation', target_field_id: 'fld_customer' },
  }),
  makeField('fld_hidden', '隐藏说明', 'text', 11),
]

function allFieldRow(): RawRow {
  return makeRow('rec_1', {
    fld_title: str('任务一'),
    fld_num: num(12.5),
    fld_status: str('opt_1'),
    fld_tags: list(str('opt_1'), str('opt_2')),
    fld_date: ts(1730000000000),
    fld_person: str('usr_1'),
    fld_attach: list(str('fil_1')),
    fld_url: { url: 'https://a.com', display_text: '官网' },
    fld_formula: int(2),
    fld_relation: str('rec_9'),
    fld_lookup: int(126),
    fld_hidden: str('仅抽屉可见'),
  })
}

describe('抽屉字段区（全字段渲染）', () => {
  beforeEach(() => {
    workspaceStore.setState({ fileMetaMap: {} })
  })

  it('按字段顺序渲染全部字段（含当前视图隐藏字段），公式与引用只读并标注', async () => {
    const rows = [allFieldRow()]
    renderDrawer({
      rows,
      fields: ALL_FIELDS,
      targetRows: [makeRow('rec_9', { fld_customer: str('客户甲') })],
    })

    expect(await screen.findByRole('dialog', { name: '记录详情' })).toBeInTheDocument()
    expect(await screen.findByTestId('drawer-field-fld_hidden')).toBeInTheDocument()
    expect(screen.getByText('仅抽屉可见')).toBeInTheDocument()

    const formula = screen.getByTestId('drawer-field-fld_formula')
    expect(within(formula).getByText('只读')).toBeInTheDocument()
    const lookup = screen.getByTestId('drawer-field-fld_lookup')
    expect(within(lookup).getByText('只读')).toBeInTheDocument()

    // 隐藏字段与普通字段均可进入编辑入口
    expect(screen.getByLabelText('编辑字段：隐藏说明')).toBeInTheDocument()
    expect(screen.queryByLabelText('编辑字段：合计')).not.toBeInTheDocument()
  })

  it('只读角色不展示任何编辑入口（仍可查看字段值）', async () => {
    renderDrawer({ rows: [allFieldRow()], fields: ALL_FIELDS, canEditCell: false })

    await screen.findByRole('dialog', { name: '记录详情' })
    expect(screen.queryByRole('button', { name: '编辑主字段：标题' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('编辑字段：隐藏说明')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('上传附件')).not.toBeInTheDocument()
  })
})

describe('抽屉内编辑提交与冲突', () => {
  beforeEach(() => {
    workspaceStore.setState({ fileMetaMap: {} })
  })

  it('主字段点击进入编辑，Enter 提交 H-51（无 client_request_id）并落版本', async () => {
    const bodies: Array<Record<string, unknown>> = []
    const query = new URLSearchParams()
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        query.set('base_id', new URL(request.url).searchParams.get('base_id') ?? '')
        return ok({ value: str('新标题'), version: '2' })
      }),
    )
    renderDrawer({ rows: [allFieldRow()], fields: ALL_FIELDS })

    await screen.findByRole('dialog', { name: '记录详情' })
    fireEvent.click(await screen.findByRole('button', { name: '编辑主字段：标题' }))
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: '新标题' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ value: '新标题', expect_version: 1 })
    expect(bodies[0]).not.toHaveProperty('client_request_id')
    expect(query.get('base_id')).toBe('base_1')
    await waitFor(() =>
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('新标题')).toBeInTheDocument()
  })

  it('行内编辑失败保留输入与行内错误', async () => {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        HttpResponse.json({ code: 9001, message: '服务暂时不可用', data: null }, { status: 500 }),
      ),
    )
    renderDrawer({ rows: [allFieldRow()], fields: ALL_FIELDS })

    await screen.findByRole('dialog', { name: '记录详情' })
    fireEvent.click(await screen.findByLabelText('编辑字段：隐藏说明'))
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: '保留内容' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(await screen.findByText(/服务暂时不可用/)).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('保留内容')
  })

  it('4001 冲突展示对方值与三动作；采用对方后落对方值', async () => {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        fail(4001, { their_value: str('对方值'), their_version: '8' }),
      ),
    )
    renderDrawer({ rows: [allFieldRow()], fields: ALL_FIELDS })

    await screen.findByRole('dialog', { name: '记录详情' })
    fireEvent.click(await screen.findByRole('button', { name: '编辑主字段：标题' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '我的值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    const conflict = (await screen.findByText('内容已被他人修改')).closest(
      '[role="dialog"]',
    ) as HTMLElement
    expect(within(conflict).getByRole('button', { name: '采用对方' })).toBeInTheDocument()
    expect(within(conflict).getByRole('button', { name: '仍用我的覆盖' })).toBeInTheDocument()
    expect(within(conflict).getByRole('button', { name: '复制我的内容' })).toBeInTheDocument()
    expect(within(conflict).getByText(/对方值/)).toBeInTheDocument()

    fireEvent.click(within(conflict).getByRole('button', { name: '采用对方' }))
    await waitFor(() => expect(screen.queryByText('内容已被他人修改')).not.toBeInTheDocument())
    expect(screen.getByText('对方值')).toBeInTheDocument()
  })

  it('4002 记录/字段已删除：保留内容可复制且不重建', async () => {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () => fail(4002)),
    )
    const { writeText } = renderDrawer({ rows: [allFieldRow()], fields: ALL_FIELDS })

    await screen.findByRole('dialog', { name: '记录详情' })
    fireEvent.click(await screen.findByRole('button', { name: '编辑主字段：标题' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '保留内容' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    const conflict = (await screen.findByText('该记录或字段已被删除')).closest(
      '[role="dialog"]',
    ) as HTMLElement
    expect(within(conflict).getByText(/保留内容/)).toBeInTheDocument()
    fireEvent.click(within(conflict).getByRole('button', { name: '复制我的内容' }))
    expect(writeText).toHaveBeenCalledWith('保留内容')
    await waitFor(() => expect(screen.queryByText('该记录或字段已被删除')).not.toBeInTheDocument())
  })
})

describe('上一条 / 下一条', () => {
  beforeEach(() => {
    workspaceStore.setState({ fileMetaMap: {} })
  })

  it('按当前视图顺序切换', async () => {
    const fields = [makeField('fld_title', '标题', 'text', 0, { isPrimary: true })]
    const rows = [
      makeRow('rec_1', { fld_title: str('记录一') }),
      makeRow('rec_2', { fld_title: str('记录二') }),
      makeRow('rec_3', { fld_title: str('记录三') }),
    ]
    renderDrawer({ rows, fields, initialRecordId: 'rec_2' })

    await screen.findByRole('dialog', { name: '记录详情' })
    expect(await screen.findByText('记录二')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '上一条' }))
    expect(await screen.findByText('记录一')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '下一条' }))
    expect(await screen.findByText('记录二')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '下一条' }))
    expect(await screen.findByText('记录三')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '下一条' })).toBeDisabled()
  })

  it('末行且 hasMore 时先补拉再切换', async () => {
    const fields = [makeField('fld_title', '标题', 'text', 0, { isPrimary: true })]
    const rows = Array.from({ length: 21 }, (_, index) =>
      makeRow(`rec_${index + 1}`, { fld_title: str(`记录${index + 1}`) }),
    )
    const cursors: Array<string | null> = []
    renderDrawer({ rows, fields, initialRecordId: 'rec_20', pageLimit: 100 })

    // 捕获第二页请求的游标
    mswServer.use(
      http.get('*/api/v1/bases/:base_id/records', ({ request }) => {
        const url = new URL(request.url)
        const cursor = url.searchParams.get('cursor')
        cursors.push(cursor)
        const offset = Number.parseInt((cursor ?? '').replace(/^cur_/, ''), 10) || 0
        const limit = Number.parseInt(url.searchParams.get('limit') ?? '20', 10) || 20
        const slice = rows.slice(offset, offset + limit)
        const hasMore = offset + limit < rows.length
        return ok({
          record_list: slice,
          has_more: hasMore,
          next_cursor: hasMore ? `cur_${offset + limit}` : null,
        })
      }),
    )

    await screen.findByRole('dialog', { name: '记录详情' })
    expect(await screen.findByText('记录20')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '下一条' }))

    expect(await screen.findByText('记录21')).toBeInTheDocument()
    expect(cursors).toContain('cur_20')
  })
})

describe('附件字段', () => {
  const attachmentFields = [
    makeField('fld_title', '标题', 'text', 0, { isPrimary: true }),
    makeField('fld_attach', '附件', 'attachment', 1),
  ]

  function attachmentRow(ids: string[]): RawRow {
    return makeRow('rec_1', {
      fld_title: str('任务一'),
      fld_attach: ids.length === 0 ? list() : list(...ids.map((id) => str(id))),
    })
  }

  beforeEach(() => {
    workspaceStore.setState({ fileMetaMap: {} })
  })

  it('展示缩略图与下载链接，上传后经 H-51 追加 file_id 并维护版本', async () => {
    workspaceStore.setState({
      fileMetaMap: { fil_1: { name: '旧附件.pdf', displayName: '旧附件.pdf' } },
    })
    let uploadForm: { contentType: string | null; baseId: string | null } | null = null
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.post('*/api/v1/files', async ({ request }) => {
        const url = new URL(request.url)
        uploadForm = {
          contentType: request.headers.get('content-type'),
          baseId: url.searchParams.get('base_id'),
        }
        return ok({
          id: 'fil_2',
          name: '新附件.txt',
          display_name: '新附件.txt',
          ext: '.txt',
          mime: 'text/plain',
          size: '5',
          created_at: '0',
        })
      }),
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        bodies.push(body)
        return ok({ value: list(str('fil_1'), str('fil_2')), version: '2' })
      }),
    )
    renderDrawer({ rows: [attachmentRow(['fil_1'])], fields: attachmentFields })

    await screen.findByRole('dialog', { name: '记录详情' })
    const thumb = await screen.findByRole('img', { name: '旧附件.pdf' })
    expect(thumb).toHaveAttribute('src', '/api/v1/files/fil_1/thumb')
    expect(screen.getByRole('link', { name: '下载附件 旧附件.pdf' })).toHaveAttribute(
      'href',
      '/api/v1/files/fil_1/download',
    )

    const file = new File(['hello'], '新附件.txt', { type: 'text/plain' })
    const input = screen.getByLabelText('上传附件') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })

    await waitFor(() => expect(uploadForm).not.toBeNull())
    const captured = uploadForm as unknown as { contentType: string | null; baseId: string | null }
    expect(captured.baseId).toBe('base_1')
    expect(captured.contentType).toContain('multipart/form-data')
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ value: ['fil_1', 'fil_2'], expect_version: 1 })
    expect(await screen.findByText('新附件.txt')).toBeInTheDocument()
  })

  it('上传网络异常统一包装 9001：抽屉显示「服务暂时不可用」而非未知错误', async () => {
    mswServer.use(http.post('*/api/v1/files', () => HttpResponse.error()))
    renderDrawer({ rows: [attachmentRow([])], fields: attachmentFields })

    await screen.findByRole('dialog', { name: '记录详情' })
    const input = await screen.findByLabelText('上传附件')
    const file = new File(['hello'], '失败.txt', { type: 'text/plain' })
    fireEvent.change(input, { target: { files: [file] } })

    expect(await screen.findByText(/服务暂时不可用/)).toBeInTheDocument()
    expect(screen.queryByText('操作失败，请稍后重试')).not.toBeInTheDocument()
  })

  it('重命名走 H-80（body {display_name}），删除走 H-81 并经 H-51 移除 file_id', async () => {
    workspaceStore.setState({
      fileMetaMap: {
        fil_1: { name: '方案.pdf', displayName: '方案.pdf' },
        fil_2: { name: '图纸.zip', displayName: '图纸.zip' },
      },
    })
    const renameBodies: Array<Record<string, unknown>> = []
    const deleted: string[] = []
    const cellBodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.patch('*/api/v1/files/:file_id', async ({ request, params }) => {
        const body = (await request.json()) as Record<string, unknown>
        renameBodies.push(body)
        return ok({
          id: String(params.file_id),
          name: '方案.pdf',
          display_name: String(body.display_name),
          ext: '.pdf',
          mime: 'application/pdf',
          size: '10',
          created_at: '0',
        })
      }),
      http.delete('*/api/v1/files/:file_id', ({ params }) => {
        deleted.push(String(params.file_id))
        return ok({})
      }),
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>
        cellBodies.push(body)
        return ok({ value: list(str('fil_2')), version: '2' })
      }),
    )
    renderDrawer({ rows: [attachmentRow(['fil_1', 'fil_2'])], fields: attachmentFields })

    await screen.findByRole('dialog', { name: '记录详情' })
    fireEvent.click(await screen.findByRole('button', { name: '重命名附件 方案.pdf' }))
    const input = screen.getByLabelText('附件新名称')
    fireEvent.change(input, { target: { value: '方案-终版.pdf' } })
    fireEvent.click(screen.getByRole('button', { name: '保存名称' }))

    await waitFor(() => expect(renameBodies).toEqual([{ display_name: '方案-终版.pdf' }]))
    expect(await screen.findByText('方案-终版.pdf')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '删除附件 方案-终版.pdf' }))
    await waitFor(() => expect(deleted).toEqual(['fil_1']))
    await waitFor(() => expect(cellBodies).toHaveLength(1))
    expect(cellBodies[0]).toEqual({ value: ['fil_2'], expect_version: 1 })
  })

  it('单记录已满 10 个附件时禁用上传并提示', async () => {
    const ids = Array.from({ length: 10 }, (_, index) => `fil_${index + 1}`)
    workspaceStore.setState({
      fileMetaMap: Object.fromEntries(
        ids.map((id) => [id, { name: `${id}.txt`, displayName: `${id}.txt` }]),
      ),
    })
    renderDrawer({ rows: [attachmentRow(ids)], fields: attachmentFields })

    await screen.findByRole('dialog', { name: '记录详情' })
    expect(await screen.findByText('每条记录最多 10 个附件')).toBeInTheDocument()
    expect(screen.getByLabelText('上传附件')).toBeDisabled()
  })

  it('单文件超过 20MB 时客户端拦截且不发请求', async () => {
    let uploads = 0
    mswServer.use(
      http.post('*/api/v1/files', () => {
        uploads += 1
        return ok({ id: 'fil_new' })
      }),
    )
    renderDrawer({ rows: [attachmentRow(['fil_1'])], fields: attachmentFields })

    await screen.findByRole('dialog', { name: '记录详情' })
    const file = new File([new Uint8Array(21 * 1024 * 1024)], '超大.bin')
    const input = (await screen.findByLabelText('上传附件')) as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })

    expect(await screen.findByText('单个文件不能超过 20MB')).toBeInTheDocument()
    expect(uploads).toBe(0)
  })

  it('服务端 5002 黑名单/超限时展示明确提示', async () => {
    mswServer.use(
      http.post('*/api/v1/files', () =>
        HttpResponse.json(
          {
            code: 5002,
            message: '文件不符合要求（类型受限或超出大小上限）',
            data: null,
          },
          { status: 413 },
        ),
      ),
    )
    renderDrawer({ rows: [attachmentRow(['fil_1'])], fields: attachmentFields })

    await screen.findByRole('dialog', { name: '记录详情' })
    const file = new File(['hello'], '黑名单.exe')
    const input = (await screen.findByLabelText('上传附件')) as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })

    expect(await screen.findByText(/文件不符合要求/)).toBeInTheDocument()
  })
})
