// 记录写入组件测试（票 06）：RTL + MSW，真实 H-50/H-51/H-52/H-53/H-54 形状。
// 覆盖：四通道进入编辑、三键语义、乐观更新与版本、失败保留输入/重试、4001 三动作、4002、
// TSV 复制/粘贴计数、批量删除 + 撤销、Ctrl+Z、拖行约束、5 万上限与三入口新增。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { ToastProvider } from '@/ui-kit'
import { mswServer } from '@/mocks/server'
import { recordsKeys } from '@/query-keys'
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
    // 写后整行刷新（票 06）默认 H-49：返回当前 harness 行（旧版本）。
    // 用例需要模拟写后服务端真值时自行 use() 覆盖（后注册者优先）。
    http.get('*/api/v1/records/:record_id', ({ params }) => {
      const row = rows.find((candidate) => candidate.id === String(params.record_id))
      if (row === undefined) {
        return HttpResponse.json(
          { code: 4002, message: '记录不存在', data: null },
          { status: 409 },
        )
      }
      return ok({ record: row })
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

    // 新增行已追加为最后一行：先取消自动聚焦的空编辑器（Esc），再在末行按方向下键触发 appendRow。
    // 票 02 后点击正在编辑的格不再丢弃草稿，因此这里用 Esc 显式退出编辑态。
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Escape' })
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

describe('失焦/点击离开提交与编辑态键盘兜底（票 02）', () => {
  it('失焦提交：编辑中失焦即发 UpdateCell（回车/失焦双通道，需求 4.6 自动保存）', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { string_value: '失焦值' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '失焦值' } })
    // 真实浏览器失焦经 focusout 冒泡（React onBlur 归一化）
    fireEvent.focusOut(screen.getByRole('textbox'))

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ value: '失焦值', expect_version: 1 })
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
    expect(within(await waitForCell('rec_1', 'fld_title')).getByText('失焦值')).toBeInTheDocument()
  })

  it('点击其他单元格提交在途草稿而不是静默丢弃（冒烟零 PUT 断点回归）', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { string_value: '点击离开值' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '点击离开值' } })
    // 真实浏览器里点击他格会先触发失焦再触发 click；jsdom 补发 blur 模拟同一序列
    fireEvent.focusOut(screen.getByRole('textbox'))
    fireEvent.click(await waitForCell('rec_2', 'fld_num'))

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]?.value).toBe('点击离开值')
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
  })

  it('点击他格提交只发一次请求（失焦与 click 不重复落盘）', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { string_value: '一次' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '一次' } })
    fireEvent.focusOut(screen.getByRole('textbox'))
    fireEvent.click(await waitForCell('rec_2', 'fld_num'))

    await waitFor(() => expect(bodies).toHaveLength(1))
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
    // 提交完成后不再追加请求
    expect(bodies).toHaveLength(1)
  })

  it('失焦未改动不发出请求（避免无意义落盘）', async () => {
    let puts = 0
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () => {
        puts += 1
        return ok({ value: { string_value: '任务一' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.focusOut(screen.getByRole('textbox'))

    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(puts).toBe(0)
  })

  it('编辑态下视口 Enter 提交在途草稿（焦点丢失时的键盘兜底）', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { string_value: '兜底值' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '兜底值' } })
    // 焦点不在编辑器输入框内（真实键鼠路径焦点丢失场景）：Enter 仍必须提交
    fireEvent.keyDown(viewport(), { key: 'Enter' })

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]?.value).toBe('兜底值')
  })

  it('编辑态下视口 Enter 未改动不发出请求：按提交语义收起并下移（与失焦跳过同源）', async () => {
    let puts = 0
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () => {
        puts += 1
        return ok({ value: { string_value: '任务一' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    // 不改动草稿：视口 Enter 必须零 PUT，但编辑器仍按 Enter 语义收起并下移
    fireEvent.keyDown(viewport(), { key: 'Enter' })

    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(puts).toBe(0)
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
    await waitFor(() =>
      expect(screen.getByTestId('cell-rec_2-fld_title')).toHaveAttribute('aria-selected', 'true'),
    )
  })

  it('编辑态下视口 Tab 未改动不发出请求：按提交语义收起并右移', async () => {
    let puts = 0
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () => {
        puts += 1
        return ok({ value: { string_value: '任务一' }, version: '2' })
      }),
    )
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    // 不改动草稿：视口 Tab 必须零 PUT，但编辑器仍按 Tab 语义收起并右移
    fireEvent.keyDown(viewport(), { key: 'Tab' })

    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(puts).toBe(0)
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
    await waitFor(() =>
      expect(screen.getByTestId('cell-rec_1-fld_num')).toHaveAttribute('aria-selected', 'true'),
    )
  })

  it('1001 提交失败：行内错误与输入内容保留、乐观值回滚到提交前（不丢内容不假保存）', async () => {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        HttpResponse.json(
          { code: 1001, message: '参数缺失或非法: value', data: null },
          { status: 400 },
        ),
      ),
    )
    const harness = renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '被拒值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    expect(await screen.findByTestId('cell-write-error')).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('被拒值')
    // 乐观值已回滚：记录缓存回到提交前值与版本（顶栏不得因此呈「已保存」以外的谎态由票 02 三态兜底）
    const data = harness.queryClient.getQueryData<{
      pages: Array<{ items: Array<{ id: string; cellsMap: Record<string, unknown>; version: number }> }>
    }>(recordsKeys.listFor('tbl_1', 'view_1', ''))
    const rec1 = data?.pages.flatMap((page) => page.items).find((row) => row.id === 'rec_1')
    expect(rec1?.cellsMap.fld_title).toBe('任务一')
    expect(rec1?.version).toBe(1)
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

describe('弹层 portal 化（票 01 / BUG-06）', () => {
  it('网格内打开单选编辑器：选项面板挂 document.body，不在网格滚动/裁剪容器内', async () => {
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_status'))

    const panel = await screen.findByRole('listbox', { name: '选项列表' })
    const grid = viewport()
    expect(panel.getAttribute('data-grid-editor-portal')).toBe('')
    expect(panel.parentElement).toBe(document.body)
    expect(grid.contains(panel)).toBe(false)
    // 单元格（overflow:hidden 裁剪上下文）同样不包含弹层
    expect(screen.getByTestId('cell-rec_1-fld_status').contains(panel)).toBe(false)
  })

  it('最后一行打开日期编辑器：日历挂 body 不被裁剪，点选日期写入单元格', async () => {
    const requests: Array<{ url: URL; body: Record<string, unknown> }> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        requests.push({
          url: new URL(request.url),
          body: (await request.json()) as Record<string, unknown>,
        })
        return ok({ value: { date_value_ms: String(Date.parse('2024-01-10')) }, version: '2' })
      }),
    )
    const fields = [
      rawField('fld_title', '标题', 'text', { is_primary: true, position: 0 }),
      rawField('fld_date', '截止日期', 'date', {
        position: 1,
        config_map: { include_time: false, format: 'YYYY-MM-DD' },
      }),
    ]
    const rows = [
      rawRow('rec_last', {
        fld_title: { string_value: '末行' },
        fld_date: { date_value_ms: String(new Date(2024, 0, 5).getTime()) },
      }),
    ]
    renderGrid({ fields, rows })
    fireEvent.doubleClick(await waitForCell('rec_last', 'fld_date'))

    const dialog = await screen.findByRole('dialog', { name: '日期选择器' })
    const grid = viewport()
    expect(dialog.getAttribute('data-grid-editor-portal')).toBe('')
    expect(dialog.parentElement).toBe(document.body)
    expect(grid.contains(dialog)).toBe(false)
    expect(screen.getByTestId('cell-rec_last-fld_date').contains(dialog)).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: '2024-01-10' }))
    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0]?.url.pathname).toContain('/cells/fld_date')
    expect(typeof requests[0]?.body.value).toBe('number')
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_last-fld_date')).not.toBeInTheDocument(),
    )
  })
})

describe('弹层归属失焦缝（票 01 portal 配套）', () => {
  it('焦点移入弹层（portal 内选项）不算离开编辑器宿主：不触发失焦提交收起', async () => {
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_status'))
    const panel = await screen.findByRole('listbox', { name: '选项列表' })
    const option = within(panel).getByRole('option', { name: '进行中' })

    // 真实浏览器点击弹层选项的 mousedown 失焦序列：focusout 冒泡到宿主，relatedTarget 在 portal 弹层内
    fireEvent.focusOut(screen.getByLabelText('搜索选项'), { relatedTarget: option })

    // 弹层属于编辑器的一部分：编辑器保持打开，不按旧草稿收起（提交链语义不变）
    expect(screen.getByTestId('editor-rec_1-fld_status')).toBeInTheDocument()
  })

  it('焦点真正离开（relatedTarget 在编辑器与弹层之外）：失焦提交缝照常收起', async () => {
    renderGrid()
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_status'))
    await screen.findByRole('listbox', { name: '选项列表' })
    const otherCell = await waitForCell('rec_2', 'fld_title')

    fireEvent.focusOut(screen.getByLabelText('搜索选项'), { relatedTarget: otherCell })

    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_status')).not.toBeInTheDocument(),
    )
  })
})

describe('写后整行刷新（票 06 BUG-05）', () => {
  it('单格提交成功后拉取整行并合并：公式列立即显示新结果，版本以服务端为准', async () => {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        ok({ value: { string_value: '新值' }, version: '2' }),
      ),
    )
    const harness = renderGrid()
    const gets: string[] = []
    mswServer.use(
      http.get('*/api/v1/records/:record_id', ({ params }) => {
        gets.push(String(params.record_id))
        return ok({
          record: rawRow(
            'rec_1',
            {
              fld_title: { string_value: '新值' },
              fld_num: { double_value: 1 },
              fld_status: { string_value: 'opt_1' },
              fld_formula: { int_value: '42' },
            },
            2,
          ),
        })
      }),
    )

    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '新值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    // 写成功后发起 H-49 单条读取
    await waitFor(() => expect(gets).toEqual(['rec_1']))
    // 公式列由「2」刷新为服务端整行返回的计算结果
    const formulaCell = await waitForCell('rec_1', 'fld_formula')
    await waitFor(() => expect(formulaCell).toHaveTextContent('42'))
    // 版本以服务端为准：缓存版本为刷新返回值
    const data = harness.queryClient.getQueryData<{
      pages: Array<{ items: Array<{ id: string; version: number; cellsMap: Record<string, unknown> }> }>
    }>(recordsKeys.listFor('tbl_1', 'view_1', ''))
    const rec1 = data?.pages.flatMap((page) => page.items).find((row) => row.id === 'rec_1')
    expect(rec1?.version).toBe(2)
    expect(rec1?.cellsMap.fld_formula).toBe(42)
  })

  it('整行刷新失败静默降级：写成功 UI 不回滚、无报错，版本保持写响应值', async () => {
    let gets = 0
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        ok({ value: { string_value: '成功值' }, version: '2' }),
      ),
    )
    const harness = renderGrid()
    mswServer.use(
      http.get('*/api/v1/records/:record_id', () => {
        gets += 1
        return HttpResponse.json(
          { code: 9001, message: '刷新失败', data: null },
          { status: 500 },
        )
      }),
    )
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_title'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '成功值' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    await waitFor(() => expect(gets).toBe(1))
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
    expect(within(await waitForCell('rec_1', 'fld_title')).getByText('成功值')).toBeInTheDocument()
    expect(screen.queryByTestId('cell-write-error')).not.toBeInTheDocument()
    // 版本不被刷新失败降级：下次编辑仍发送 2
    const data = harness.queryClient.getQueryData<{
      pages: Array<{ items: Array<{ id: string; version: number }> }>
    }>(recordsKeys.listFor('tbl_1', 'view_1', ''))
    const rec1 = data?.pages.flatMap((page) => page.items).find((row) => row.id === 'rec_1')
    expect(rec1?.version).toBe(2)
  })

  it('批量粘贴成功后受影响行整行刷新', async () => {
    const gets: string[] = []
    mswServer.use(
      http.post('*/api/v1/records/batch', () =>
        ok({
          result_list: [{ index: 0, record_id: 'rec_1', ok: true, version: '3' }],
          fail_row_list: [],
          skipped_count: 0,
        }),
      ),
    )
    const harness = renderGrid()
    harness.readText.mockResolvedValue('粘贴值')
    mswServer.use(
      http.get('*/api/v1/records/:record_id', ({ params }) => {
        gets.push(String(params.record_id))
        return ok({
          record: rawRow(
            'rec_1',
            { fld_title: { string_value: '粘贴值' }, fld_formula: { int_value: '42' } },
            3,
          ),
        })
      }),
    )

    fireEvent.click(await waitForCell('rec_1', 'fld_title'))
    fireEvent.keyDown(viewport(), { key: 'v', ctrlKey: true })

    await waitFor(() => expect(gets).toEqual(['rec_1']))
    const formulaCell = await waitForCell('rec_1', 'fld_formula')
    await waitFor(() => expect(formulaCell).toHaveTextContent('42'))
  })

  it('新建记录后拉取整行：公式列立即呈现计算结果而非「计算中…」', async () => {
    let gets = 0
    mswServer.use(
      http.post('*/api/v1/bases/:base_id/records', () =>
        ok({ record: rawRow('rec_new', {}) }),
      ),
    )
    renderGrid()
    mswServer.use(
      http.get('*/api/v1/records/:record_id', () => {
        gets += 1
        return ok({
          record: rawRow(
            'rec_new',
            { fld_title: { string_value: '' }, fld_formula: { int_value: '7' } },
            1,
          ),
        })
      }),
    )

    fireEvent.click(await screen.findByRole('button', { name: '首行新增记录' }))

    await waitFor(() => expect(gets).toBe(1))
    const formulaCell = await waitForCell('rec_new', 'fld_formula')
    await waitFor(() => expect(formulaCell).toHaveTextContent('7'))
  })

  it('撤销删除恢复后整行刷新：恢复行的公式列同步更新', async () => {
    uiStore.getState().clearUndoWindow()
    const rows = [
      rawRow('rec_1', { fld_title: { string_value: '任务一' }, fld_formula: { int_value: '2' } }),
      rawRow('rec_2', { fld_title: { string_value: '任务二' } }),
    ]
    let gets = 0
    const harness = renderGrid({ rows })
    await waitForCell('rec_1', 'fld_title')
    mswServer.use(
      http.delete('*/api/v1/records', () => {
        const index = rows.findIndex((row) => row.id === 'rec_1')
        rows.splice(index, 1)
        return ok({ deleted_count: 1 })
      }),
      http.post('*/api/v1/records/:record_id/undo', ({ params }) => {
        const restored = rawRow(
          String(params.record_id),
          { fld_title: { string_value: '任务一' }, fld_formula: { int_value: '9' } },
          5,
        )
        rows.push(restored)
        return ok({ record: restored })
      }),
      http.get('*/api/v1/records/:record_id', () => {
        gets += 1
        return ok({
          record: rawRow(
            'rec_1',
            { fld_title: { string_value: '任务一' }, fld_formula: { int_value: '9' } },
            5,
          ),
        })
      }),
    )

    fireEvent.click(screen.getByRole('checkbox', { name: '选择记录 rec_1' }))
    harness.rerender({ command: { kind: 'delete-selected', token: 1 } })
    await waitFor(() =>
      expect(screen.queryByTestId('cell-rec_1-fld_title')).not.toBeInTheDocument(),
    )
    harness.rerender({ command: { kind: 'undo-delete', token: 2 } })

    await waitFor(() => expect(gets).toBe(1))
    const formulaCell = await waitForCell('rec_1', 'fld_formula')
    await waitFor(() => expect(formulaCell).toHaveTextContent('9'))
    expect(uiStore.getState().undoWindow).toBeNull()
  })
})

// -------------------------------------------------------------------------------------------
// 下拉编辑器点击选中与提交链（票 02 / BUG-01+BUG-04 挂载级）：既有编辑器用例只走键盘路径，
// 逃逸了「点击 → 焦点转移 → 失焦提交」竞态。本组用例在挂载网格 + MSW 写链下走真实 jsdom
// focus/blur 链路（显式 .focus() 模拟浏览器 mousedown 抢焦），断言点击选项后的 PUT 与单元格呈现。
// -------------------------------------------------------------------------------------------

/** 下拉用例字段：主字段 + 单选（fld_status）+ 多选（fld_tags），同 option_list 便于对照。 */
const DROPDOWN_FIELDS: RawField[] = [
  rawField('fld_title', '标题', 'text', { is_primary: true, position: 0 }),
  rawField('fld_status', '状态', 'select', {
    position: 1,
    config_map: {
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
      allow_create: false,
    },
  }),
  rawField('fld_tags', '标签', 'multiselect', {
    position: 2,
    config_map: {
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
      allow_create: false,
    },
  }),
]

/** 下拉用例行：单选/多选单元格均为空值。 */
const DROPDOWN_ROWS: RawRow[] = [
  rawRow('rec_1', { fld_title: { string_value: '任务一' } }),
  rawRow('rec_2', { fld_title: { string_value: '任务二' } }),
]

describe('下拉编辑器点击选中与提交链（票 02 / BUG-01+BUG-04 挂载级）', () => {
  it('网格内单选：真实点击选项即提交（PUT option_id）并收起编辑器，单元格立即呈现标签', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { string_value: 'opt_2' }, version: '2' })
      }),
    )
    renderGrid({ fields: DROPDOWN_FIELDS, rows: DROPDOWN_ROWS })
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_status'))
    const input = screen.getByLabelText('搜索选项')
    act(() => {
      input.focus()
    })

    // 真实浏览器点击选项 = mousedown 抢焦（焦点进入 portal 弹层，宿主失焦被视为未离开）→ click
    const option = within(screen.getByRole('listbox', { name: '选项列表' })).getByRole('option', {
      name: '完成',
    })
    act(() => {
      option.focus()
    })
    fireEvent.click(option)

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ value: 'opt_2', expect_version: 1 })
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_status')).not.toBeInTheDocument(),
    )
    expect(within(await waitForCell('rec_1', 'fld_status')).getByText('完成')).toBeInTheDocument()
  })

  it('网格内多选：连勾两条后失焦提交集合（PUT 数组），编辑器收起且单元格显示全部标签', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({
          value: {
            list_value: { value_list: [{ string_value: 'opt_1' }, { string_value: 'opt_2' }] },
          },
          version: '2',
        })
      }),
    )
    renderGrid({ fields: DROPDOWN_FIELDS, rows: DROPDOWN_ROWS })
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_tags'))
    const input = screen.getByLabelText('搜索选项')
    act(() => {
      input.focus()
    })
    const panel = screen.getByRole('listbox', { name: '选项列表' })

    // 连续点击两条：勾选切换、编辑器保持打开（期间不发生 PUT）
    fireEvent.click(within(panel).getByRole('option', { name: /进行中/ }))
    fireEvent.click(within(panel).getByRole('option', { name: /完成/ }))
    expect(within(panel).getByRole('option', { name: /进行中/ })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(within(panel).getByRole('option', { name: /完成/ })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(bodies).toHaveLength(0)

    // 真实失焦链：焦点移出编辑器到另一单元格 → 失焦提交最新草稿（勾选集合）
    act(() => {
      screen.getByTestId('cell-rec_2-fld_title').focus()
    })

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ value: ['opt_1', 'opt_2'], expect_version: 1 })
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_tags')).not.toBeInTheDocument(),
    )
    const cell = await waitForCell('rec_1', 'fld_tags')
    expect(within(cell).getByText('进行中')).toBeInTheDocument()
    expect(within(cell).getByText('完成')).toBeInTheDocument()
  })

  it('网格内多选：搜索词+回车只勾选匹配项、编辑器保持打开，不提交旧集合（零 PUT）', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { list_value: { value_list: [{ string_value: 'opt_2' }] } }, version: '2' })
      }),
    )
    renderGrid({ fields: DROPDOWN_FIELDS, rows: DROPDOWN_ROWS })
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_tags'))
    const input = screen.getByLabelText('搜索选项')

    fireEvent.change(input, { target: { value: '完成' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    // 编辑器保持打开、该选项被勾选、搜索词清空，且未发出任何写请求（旧集合不落盘）
    expect(screen.getByTestId('editor-rec_1-fld_tags')).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /完成/ })).toHaveAttribute('aria-selected', 'true')
    expect(input).toHaveValue('')
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(bodies).toHaveLength(0)
  })

  it('网格内单选：方向键移动高亮（aria-activedescendant）且回车选中高亮项落盘', async () => {
    const bodies: Array<Record<string, unknown>> = []
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return ok({ value: { string_value: 'opt_1' }, version: '2' })
      }),
    )
    renderGrid({ fields: DROPDOWN_FIELDS, rows: DROPDOWN_ROWS })
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_status'))
    const input = screen.getByLabelText('搜索选项')

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    const highlighted = screen.getByRole('option', { name: '进行中' })
    expect(input.getAttribute('aria-activedescendant')).toBe(highlighted.getAttribute('id'))

    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ value: 'opt_1', expect_version: 1 })
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_status')).not.toBeInTheDocument(),
    )
    expect(within(await waitForCell('rec_1', 'fld_status')).getByText('进行中')).toBeInTheDocument()
  })

  it('网格内单选：方向键高亮后 Esc 取消不落盘（编辑器收起、零 PUT）', async () => {
    let puts = 0
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () => {
        puts += 1
        return ok({ value: { string_value: 'opt_1' }, version: '2' })
      }),
    )
    renderGrid({ fields: DROPDOWN_FIELDS, rows: DROPDOWN_ROWS })
    fireEvent.doubleClick(await waitForCell('rec_1', 'fld_status'))
    const input = screen.getByLabelText('搜索选项')

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Escape' })

    expect(screen.queryByTestId('editor-rec_1-fld_status')).not.toBeInTheDocument()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(puts).toBe(0)
  })
})
