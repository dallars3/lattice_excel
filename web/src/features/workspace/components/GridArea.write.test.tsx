// 记录写入组件测试（票 06）：RTL + MSW，真实 H-50/H-51/H-52/H-53/H-54 形状。
// 覆盖：四通道进入编辑、三键语义、乐观更新与版本、失败保留输入/重试、4001 三动作、4002、
// TSV 复制/粘贴计数、批量删除 + 撤销、Ctrl+Z、拖行约束、5 万上限与三入口新增。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { ToastProvider } from '@/ui-kit'
import { mswServer } from '@/mocks/server'
import { uiStore } from '@/stores'

import type { FieldRecord } from '../api/field'
import { GridArea, type GridAreaProps, type RecordCommand } from './GridArea'

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
    type: `FIELD_TYPE_${type.toUpperCase()}`,
    config_map: {},
    is_primary: false,
    position: 0,
    description: '',
    ...overrides,
  }
}

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

const FIELDS: RawField[] = [
  rawField('fld_title', '标题', 'text', { is_primary: true, position: 0 }),
  rawField('fld_num', '金额', 'number', { position: 1, config_map: { format: 'decimal', decimals: 2 } }),
  rawField('fld_status', '状态', 'select', {
    position: 2,
    config_map: {
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
      allow_create: false,
    },
  }),
  rawField('fld_formula', '合计', 'formula', { position: 3, config_map: { expression: 'SUM' } }),
]

interface RawRow {
  id: string
  table_id: string
  cells_map: Record<string, unknown>
  created_by: string
  created_at: string
  version: string
}

function rawRow(id: string, cellsMap: Record<string, unknown>, version = 1): RawRow {
  return {
    id,
    table_id: 'tbl_1',
    cells_map: cellsMap,
    created_by: 'usr_1',
    created_at: '0',
    version: String(version),
  }
}

function defaultRows(): RawRow[] {
  return [
    rawRow('rec_1', {
      fld_title: { string_value: '任务一' },
      fld_num: { double_value: 1 },
      fld_status: { string_value: 'opt_1' },
      fld_formula: { int_value: '2' },
    }),
    rawRow('rec_2', {
      fld_title: { string_value: '任务二' },
      fld_num: { double_value: 2 },
      fld_status: { string_value: 'opt_2' },
    }),
  ]
}

interface Harness {
  queryClient: QueryClient
  rows: RawRow[]
  writeText: ReturnType<typeof vi.fn>
  readText: ReturnType<typeof vi.fn>
  rerender: (next: Partial<GridAreaProps> & { command?: RecordCommand | null }) => void
}

function renderGrid(
  options: {
    fields?: RawField[]
    rows?: RawRow[]
    props?: Partial<GridAreaProps>
    onSelectedRecordsChange?: (ids: readonly string[]) => void
  } = {},
): Harness {
  const fields = options.fields ?? FIELDS
  const rows = options.rows ?? defaultRows()
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const writeText = vi.fn(async () => undefined)
  const readText = vi.fn(async () => '')
  const clipboard = { writeText, readText }

  mswServer.use(
    http.get('*/api/v1/bases/:base_id/records', ({ request }) => {
      const url = new URL(request.url)
      if (url.searchParams.get('table_id') === 'tbl_2') {
        return ok({ record_list: [], has_more: false, next_cursor: null })
      }
      return ok({ record_list: rows, has_more: false, next_cursor: null })
    }),
    http.get('*/api/v1/bases/:base_id/shares/members', () =>
      ok({ collaborator_list: [{ user_id: 'usr_1', display_name: '张三' }] }),
    ),
    http.get('*/api/v1/tables/:table_id/fields', ({ params }) =>
      ok({
        field_list:
          String(params.table_id) === 'tbl_2'
            ? [rawField('fld_name', '客户名', 'text', { table_id: 'tbl_2', is_primary: true })]
            : fields,
      }),
    ),
    http.get('*/api/v1/tables/:table_id/views', () =>
      ok({ view_list: [{ id: 'view_2', table_id: 'tbl_2', name: '默认', type: 'table', created_at: '0' }] }),
    ),
  )

  const baseProps: GridAreaProps = {
    tableId: 'tbl_1',
    baseId: 'base_1',
    viewId: 'view_1',
    fields: fields.map(normalize),
    canManageStructure: true,
    canEditFields: true,
    clipboard,
    onSelectedRecordsChange: options.onSelectedRecordsChange,
    ...options.props,
  }

  function renderTree(props: GridAreaProps) {
    return (
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <GridArea {...props} />
        </ToastProvider>
      </QueryClientProvider>
    )
  }

  const view = render(renderTree(baseProps))

  return {
    queryClient,
    rows,
    writeText,
    readText,
    rerender: (next) => {
      const { command, ...rest } = next
      view.rerender(
        renderTree({
          ...baseProps,
          ...rest,
          onSelectedRecordsChange: next.onSelectedRecordsChange ?? options.onSelectedRecordsChange,
          recordCommand: command === undefined ? baseProps.recordCommand : command,
        }),
      )
    },
  }
}

function viewport(): HTMLElement {
  return screen.getByRole('grid', { name: '记录网格' })
}

async function waitForCell(recordId: string, fieldId: string): Promise<HTMLElement> {
  return screen.findByTestId(`cell-${recordId}-${fieldId}`)
}

describe('进入编辑的四条通道（AC-REC-01）', () => {
  it('单击仅选中；双击/F2/Enter/直接打字进入编辑', async () => {
    renderGrid()
    const cell = await waitForCell('rec_1', 'fld_title')

    fireEvent.click(cell)
    expect(within(cell).queryByRole('textbox')).not.toBeInTheDocument()

    fireEvent.doubleClick(cell)
    expect(screen.getByTestId('editor-rec_1-fld_title')).toBeInTheDocument()
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' })
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )

    fireEvent.click(cell)
    fireEvent.keyDown(viewport(), { key: 'F2' })
    expect(screen.getByTestId('editor-rec_1-fld_title')).toBeInTheDocument()
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' })

    fireEvent.click(cell)
    fireEvent.keyDown(viewport(), { key: 'Enter' })
    expect(screen.getByTestId('editor-rec_1-fld_title')).toBeInTheDocument()
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' })

    fireEvent.click(cell)
    fireEvent.keyDown(viewport(), { key: 'A' })
    expect(screen.getByRole('textbox')).toHaveValue('A')
  })

  it('公式列只读：双击不进入编辑', async () => {
    renderGrid()
    const cell = await waitForCell('rec_1', 'fld_formula')
    fireEvent.doubleClick(cell)
    expect(screen.queryByTestId('editor-rec_1-fld_formula')).not.toBeInTheDocument()
  })
})

describe('提交语义（AC-REC-02）与乐观更新', () => {
  it('Enter 提交并下移：PUT 载荷/查询正确（无 client_request_id），本地立即生效并落版本', async () => {
    const requests: Array<{ url: URL; body: Record<string, unknown> }> = []
    let release: () => void = () => undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        requests.push({
          url: new URL(request.url),
          body: (await request.json()) as Record<string, unknown>,
        })
        await gate
        return ok({ value: { string_value: '新值' }, version: '2' })
      }),
    )
    const harness = renderGrid()
    const cell = await waitForCell('rec_1', 'fld_title')
    fireEvent.doubleClick(cell)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '新值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    // 乐观：请求尚未返回时编辑器已持有新草稿，本地值已应用（无额外重拉）
    expect(screen.getByRole('textbox')).toHaveValue('新值')

    release()
    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]?.url.searchParams.get('base_id')).toBe('base_1')
    expect(requests[0]?.body).toEqual({ value: '新值', expect_version: 1 })
    expect(requests[0]?.body).not.toHaveProperty('client_request_id')
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
    // 提交后焦点下移
    expect(screen.getByTestId('cell-rec_2-fld_title')).toHaveAttribute('aria-selected', 'true')

    // 版本已落：再次编辑同一记录发送 version=2
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '第二值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
    await waitFor(() => expect(requests).toHaveLength(2))
    expect(requests[1]?.body.expect_version).toBe(2)
    expect(harness.queryClient).toBeDefined()
  })

  it('Tab 提交右移、Shift+Tab 左移、Esc 取消不落盘', async () => {
    const bodies: unknown[] = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push(await request.json())
        return ok({ value: { string_value: 'X' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'X' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Tab' })
    await waitFor(() => expect(bodies).toHaveLength(1))
    await waitFor(() =>
      expect(screen.getByTestId('cell-rec_1-fld_num')).toHaveAttribute('aria-selected', 'true'),
    )

    // Shift+Tab 提交并左移
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_num'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '5' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Tab', shiftKey: true })
    await waitFor(() => expect(bodies).toHaveLength(2))
    await waitFor(() =>
      expect(screen.getByTestId('cell-rec_1-fld_title')).toHaveAttribute('aria-selected', 'true'),
    )

    // Esc 取消：不发请求
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' })
    expect(bodies).toHaveLength(2)
  })

  it('提交失败：保留编辑器与输入、行内错误与重试按钮；重试成功后关闭', async () => {
    let attempts = 0
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () => {
        attempts += 1
        return attempts === 1
          ? HttpResponse.json(
              { code: 9001, message: '服务暂时不可用', data: null },
              { status: 500 },
            )
          : ok({ value: { string_value: '重试值' }, version: '4' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '重试值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    expect(await screen.findByTestId('cell-write-error')).toHaveTextContent('服务暂时不可用')
    expect(screen.getByRole('textbox')).toHaveValue('重试值')
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(attempts).toBe(2))
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
  })
})

describe('冲突弹窗（4001 / 4002）', () => {
  function mockConflict() {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        HttpResponse.json(
          {
            code: 4001,
            message: '该单元格已被另一协作者修改',
            data: { their_value: { string_value: 'B' }, their_version: '8' },
          },
          { status: 409 },
        ),
      ),
    )
  }

  it('4001 采用对方：落对方值与版本，不静默覆盖我的编辑', async () => {
    mockConflict()
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '我的值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('该单元格已被修改为「B」。')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '采用对方' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(within(await waitForCell('rec_1', 'fld_title')).getByText('B')).toBeInTheDocument()
    expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument()
  })

  it('4001 仍用我的覆盖：携对方版本重放并落我的值', async () => {
    mockConflict()
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '我的值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    const dialog = await screen.findByRole('dialog')
    let overwriteBody: Record<string, unknown> | null = null
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        overwriteBody = (await request.json()) as Record<string, unknown>
        return ok({ value: { string_value: '我的值' }, version: '9' })
      }),
    )
    fireEvent.click(within(dialog).getByRole('button', { name: '仍用我的覆盖' }))

    await waitFor(() => expect(overwriteBody).toEqual({ value: '我的值', expect_version: 8 }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(within(await waitForCell('rec_1', 'fld_title')).getByText('我的值')).toBeInTheDocument()
  })

  it('4001 复制我的内容：写入剪贴板并保留编辑态', async () => {
    mockConflict()
    const harness = renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '我的值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '复制我的内容' }))
    expect(harness.writeText).toHaveBeenCalledWith('我的值')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByTestId('editor-rec_1-fld_title')).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('我的值')
  })

  it('4002 记录/字段已删除：说明原因、内容可复制、不自动重建', async () => {
    let created = 0
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        HttpResponse.json({ code: 4002, message: '记录不存在', data: null }, { status: 409 }),
      ),
      http.post('*/api/v1/bases/:base_id/records', () => {
        created += 1
        return ok({ record: rawRow('rec_x', {}) })
      }),
    )
    const harness = renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '保留内容' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('heading', { name: '该记录或字段已被删除' })).toBeInTheDocument()
    expect(within(dialog).getByText(/不会自动重建/)).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '复制我的内容' }))
    expect(harness.writeText).toHaveBeenCalledWith('保留内容')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(created).toBe(0)
  })
})

describe('TSV 复制与粘贴（AC-REC-04/05）', () => {
  it('选区复制为制表符分隔文本', async () => {
    const harness = renderGrid()
    fireEvent.click(await waitForCell('rec_1', 'fld_title'))
    fireEvent.keyDown(viewport(), { key: 'ArrowRight', shiftKey: true })
    fireEvent.keyDown(viewport(), { key: 'c', ctrlKey: true })

    await waitFor(() => expect(harness.writeText).toHaveBeenCalledWith('任务一\t1'))
  })

  it('单行粘贴经 H-52 落库；只读列跳过并计入提示', async () => {
    const captured: { body: Record<string, unknown> | null } = { body: null }
    mswServer.use(
      http.post('*/api/v1/records/batch', async ({ request }) => {
        captured.body = (await request.json()) as Record<string, unknown>
        return ok({ result_list: [], fail_row_list: [], skipped_count: 0 })
      }),
    )
    const harness = renderGrid()
    harness.readText.mockResolvedValue('你好\t7\t进行中\t=SUM()')
    fireEvent.click(await waitForCell('rec_1', 'fld_title'))
    fireEvent.keyDown(viewport(), { key: 'v', ctrlKey: true })

    await waitFor(() => expect(captured.body).not.toBeNull())
    const list = captured.body?.op_list as Array<Record<string, unknown>>
    expect(list.length).toBe(3)
    expect(list[0]).toMatchObject({ field_id: 'fld_title', value: '你好', expect_version: 0 })
    expect(list[1]).toMatchObject({ field_id: 'fld_num', value: 7 })
    expect(list[2]).toMatchObject({ field_id: 'fld_status', value: 'opt_1' })
    await waitFor(() =>
      expect(screen.getByText(/已粘贴 3 个单元格；跳过只读列 1 个/)).toBeInTheDocument(),
    )
  })

  it('粘贴成功后写回 H-52 版本：紧接着编辑同一记录使用新版本而非旧缓存', async () => {
    const batchBodies: Array<Record<string, unknown>> = []
    const putBodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.post('*/api/v1/records/batch', async ({ request }) => {
        batchBodies.push((await request.json()) as Record<string, unknown>)
        return ok({
          result_list: [{ index: 0, record_id: 'rec_1', ok: true, version: '3' }],
          fail_row_list: [],
          skipped_count: 0,
        })
      }),
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        putBodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { string_value: '第二值' }, version: '4' })
      }),
    )
    const harness = renderGrid()
    harness.readText.mockResolvedValue('粘贴值')
    fireEvent.click(await waitForCell('rec_1', 'fld_title'))
    fireEvent.keyDown(viewport(), { key: 'v', ctrlKey: true })

    await waitFor(() => expect(batchBodies).toHaveLength(1))
    await screen.findByText('已粘贴 1 个单元格')
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '第二值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    await waitFor(() => expect(putBodies).toHaveLength(1))
    expect(putBodies[0]?.expect_version).toBe(3)
  })

  it('二维区域粘贴按 G-5 提示且不发请求', async () => {
    let batchCalls = 0
    mswServer.use(
      http.post('*/api/v1/records/batch', () => {
        batchCalls += 1
        return ok({ result_list: [], fail_row_list: [], skipped_count: 0 })
      }),
    )
    const harness = renderGrid()
    harness.readText.mockResolvedValue('a\nb')
    fireEvent.click(await waitForCell('rec_1', 'fld_title'))
    fireEvent.keyDown(viewport(), { key: 'v', ctrlKey: true })

    expect(await screen.findByText('当前仅支持单值或单行粘贴')).toBeInTheDocument()
    expect(batchCalls).toBe(0)
  })
})

describe('删除、撤销与 Ctrl+Z', () => {
  it('勾选两行后批量删除（H-53），记录 7 秒撤销窗口；撤销经 H-54 恢复', async () => {
    uiStore.getState().clearUndoWindow()
    let deleteBody: Record<string, unknown> | null = null
    const undone: string[] = []
    mswServer.use(
      http.delete('*/api/v1/records', async ({ request }) => {
        deleteBody = (await request.json()) as Record<string, unknown>
        return ok({ deleted_count: 2 })
      }),
      http.post('*/api/v1/records/:record_id/undo', ({ params }) => {
        undone.push(String(params.record_id))
        return ok({ record: rawRow(String(params.record_id), {}) })
      }),
    )
    const harness = renderGrid()
    await waitForCell('rec_1', 'fld_title')
    fireEvent.click(screen.getByRole('checkbox', { name: '选择记录 rec_1' }))
    fireEvent.click(screen.getByRole('checkbox', { name: '选择记录 rec_2' }))

    harness.rerender({ command: { kind: 'delete-selected', token: 1 } })
    await waitFor(() =>
      expect(deleteBody).toMatchObject({ record_id_list: ['rec_1', 'rec_2'] }),
    )
    await waitFor(() => expect(uiStore.getState().undoWindow?.recordIds).toEqual(['rec_1', 'rec_2']))

    harness.rerender({ command: { kind: 'undo-delete', token: 2 } })
    await waitFor(() => expect(undone.sort()).toEqual(['rec_1', 'rec_2']))
    expect(uiStore.getState().undoWindow).toBeNull()
  })

  it('Ctrl+Z 撤销会话内最近一步编辑（写回 before 值 + 当前版本）', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({
          value: { string_value: bodies.length === 1 ? '已修改' : '任务一' },
          version: String(bodies.length + 1),
        })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '已修改' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
    await waitFor(() => expect(bodies).toHaveLength(1))

    fireEvent.keyDown(viewport(), { key: 'z', ctrlKey: true })
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]).toEqual({ value: '任务一', expect_version: 2 })
    expect(await screen.findByText(/已撤销最近一步编辑/)).toBeInTheDocument()
  })
})

describe('新增记录的三条入口与容量上限', () => {
  function mockCreate() {
    const bodies: Array<Record<string, unknown>> = []
    let created = 0
    mswServer.use(
      http.post('*/api/v1/bases/:base_id/records', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        created += 1
        const id = created === 1 ? 'rec_new' : `rec_new_${created}`
        return ok({ record: rawRow(id, { fld_title: { string_value: '' } }) })
      }),
    )
    return bodies
  }

  it('底栏命令新增：POST table_id + cells_map，追加缓存后焦点落主字段并进入编辑', async () => {
    const harness = renderGrid()
    await waitForCell('rec_1', 'fld_title')
    const bodies = mockCreate()
    harness.rerender({ command: { kind: 'create', token: 1 } })

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]?.table_id).toBe('tbl_1')
    expect(bodies[0]?.cells_map).toEqual({})
    expect(typeof bodies[0]?.client_request_id).toBe('string')
    await waitFor(() =>
      expect(screen.getByTestId('editor-rec_new-fld_title')).toBeInTheDocument(),
    )
  })

  it('首屏已满 20 条时新增：记录追加到缓存并立即可见，主字段进入编辑（不依赖重拉）', async () => {
    const rows = Array.from({ length: 20 }, (_, index) =>
      rawRow(`rec_${index + 1}`, { fld_title: { string_value: `任务${index + 1}` } }),
    )
    let recordsGet = 0
    mswServer.use(
      http.get('*/api/v1/bases/:base_id/records', () => {
        recordsGet += 1
        return ok({ record_list: rows, has_more: false, next_cursor: null })
      }),
      http.post('*/api/v1/bases/:base_id/records', () =>
        ok({ record: rawRow('rec_new', { fld_title: { string_value: '' } }) }),
      ),
    )
    renderGrid({ rows })
    await waitForCell('rec_1', 'fld_title')
    const getsBefore = recordsGet

    fireEvent.click(await screen.findByRole('button', { name: '首行新增记录' }))

    await waitFor(() =>
      expect(screen.getByTestId('editor-rec_new-fld_title')).toBeInTheDocument(),
    )
    expect(recordsGet).toBe(getsBefore)
  })

  it('首行幽灵行与末行方向下键均可新增', async () => {
    const bodies = mockCreate()
    renderGrid()
    await waitForCell('rec_1', 'fld_title')
    fireEvent.click(await screen.findByRole('button', { name: '首行新增记录' }))
    await waitFor(() => expect(bodies).toHaveLength(1))

    // 新增行已追加为最后一行：在它上按方向下键才会触发 appendRow。
    fireEvent.click(await waitForCell('rec_new', 'fld_title'))
    fireEvent.keyDown(viewport(), { key: 'ArrowDown' })
    await waitFor(() => expect(bodies).toHaveLength(2))
  })

  it('服务端 5001 时提示联系管理者且不落数据', async () => {
    mswServer.use(
      http.post('*/api/v1/bases/:base_id/records', () =>
        HttpResponse.json({ code: 5001, message: '已达容量上限', data: null }, { status: 413 }),
      ),
    )
    renderGrid()
    fireEvent.click(await screen.findByRole('button', { name: '首行新增记录' }))
    expect(await screen.findByText('已达单表 5 万行上限，请联系管理者')).toBeInTheDocument()
  })
})

describe('手动排序约束（AC-REC-08）', () => {
  function rowIds(): string[] {
    return Array.from(document.querySelectorAll('[data-record-id]')).map(
      (element) => element.getAttribute('data-record-id') ?? '',
    )
  }

  it('无排序/分组时可拖行调整本地行序', async () => {
    renderGrid({ props: { hasSortOrGroup: false } })
    await waitForCell('rec_1', 'fld_title')
    const rows = Array.from(document.querySelectorAll('[data-record-id]'))
    fireEvent.dragStart(rows[0] as HTMLElement)
    fireEvent.dragOver(rows[1] as HTMLElement)
    fireEvent.drop(rows[1] as HTMLElement)

    expect(rowIds()).toEqual(['rec_2', 'rec_1'])
  })

  it('有排序/分组时拖行被禁止并提示「当前为排序视图」', async () => {
    renderGrid({ props: { hasSortOrGroup: true } })
    await waitForCell('rec_1', 'fld_title')
    const rows = Array.from(document.querySelectorAll('[data-record-id]'))
    fireEvent.dragStart(rows[0] as HTMLElement)
    fireEvent.drop(rows[1] as HTMLElement)

    expect(screen.getByRole('alert')).toHaveTextContent('当前为排序视图')
    expect(rowIds()).toEqual(['rec_1', 'rec_2'])
  })
})

describe('选项新建权限（H-40 仅 manager）', () => {
  it('editor 输入无匹配选项时提示联系管理员，且不发生 H-40 请求', async () => {
    let h40 = 0
    mswServer.use(
      http.put('*/api/v1/fields/:field_id/options', () => {
        h40 += 1
        return ok({ field: rawField('fld_status', '状态', 'select') })
      }),
    )
    const fields = [
      rawField('fld_title', '标题', 'text', { is_primary: true, position: 0 }),
      rawField('fld_status', '状态', 'select', {
        position: 1,
        config_map: {
          option_list: [{ id: 'opt_1', name: '进行中', color: 'blue' }],
          allow_create: true,
        },
      }),
    ]
    const rows = [rawRow('rec_1', { fld_title: { string_value: '任务一' } })]
    renderGrid({ fields, rows, props: { canManageStructure: false } })

    const cell = await waitForCell('rec_1', 'fld_status')
    fireEvent.doubleClick(cell)
    const input = screen.getByRole('combobox')
    fireEvent.change(input, { target: { value: '新选项' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(await screen.findAllByText('无匹配选项，请联系表格管理员添加')).not.toHaveLength(0)
    expect(h40).toBe(0)
  })
})

describe('可填写权限（只编辑自己创建的记录）', () => {
  it('记录非本人创建时双击不进入编辑', async () => {
    const rows = [
      rawRow('rec_other', { fld_title: { string_value: '他人记录' } }),
    ]
    renderGrid({
      rows,
      props: { editOwnOnly: true, currentUserId: 'usr_2' },
    })
    fireEvent.doubleClick(await waitForCell('rec_other', 'fld_title'))
    expect(screen.queryByTestId('editor-rec_other-fld_title')).not.toBeInTheDocument()
  })
})

describe('底栏命令与批量复制', () => {
  it('复制记录 = H-49 读 + H-50 建（逐条）', async () => {
    let created = 0
    mswServer.use(
      http.get('*/api/v1/records/:record_id', () =>
        ok({
          record: rawRow('rec_1', {
            fld_title: { string_value: '任务一' },
            fld_num: { double_value: 1 },
            fld_status: { string_value: 'opt_1' },
            fld_formula: { int_value: '2' },
          }),
        }),
      ),
      http.post('*/api/v1/bases/:base_id/records', () => {
        created += 1
        return ok({ record: rawRow(`rec_copy_${created}`, {}) })
      }),
    )
    const harness = renderGrid()
    await waitForCell('rec_1', 'fld_title')
    fireEvent.click(screen.getByRole('checkbox', { name: '选择记录 rec_1' }))
    harness.rerender({ command: { kind: 'copy-selected', token: 1 } })

    await waitFor(() => expect(created).toBe(1))
  })
})

describe('底栏批量条与撤销窗口（组件）', () => {
  it('勾选行后显示批量操作条（由父组件消费）', async () => {
    const changes = vi.fn()
    renderGrid({ onSelectedRecordsChange: changes })
    await waitForCell('rec_1', 'fld_title')
    fireEvent.click(screen.getByRole('checkbox', { name: '选择记录 rec_1' }))
    await waitFor(() => expect(changes).toHaveBeenLastCalledWith(['rec_1']))
  })
})
