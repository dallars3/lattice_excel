// 记录网格组件测试（票 05）：RTL + MSW，真实 H-48 形状（protojson common.Value 包装）。
// 覆盖首屏 20 条、滚动 cursor 链/limit 100、11 类渲染取值、元数据映射与失败降级、
// 四类空态/错误态与底栏计数回调。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { lookupErrorCatalog } from '@/core/http'
import { mswServer } from '@/mocks/server'
import { workspaceStore } from '@/stores'
import { ToastProvider } from '@/ui-kit'

import type { FieldRecord } from '../api/field'
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

function fail(code: number) {
  const entry = lookupErrorCatalog(code)
  return HttpResponse.json({ code, message: entry.title, data: null }, { status: entry.httpStatus })
}

interface RenderOptions {
  fields?: RawField[]
  props?: Partial<GridAreaProps>
}

function renderGrid(options: RenderOptions = {}) {
  const fields = options.fields ?? [rawField('fld_title', '标题', 'text', { is_primary: true })]
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <GridArea
          tableId="tbl_1"
          baseId="base_1"
          viewId="view_1"
          fields={fields.map(normalize)}
          canManageStructure
          canEditFields
          {...options.props}
        />
      </ToastProvider>
    </QueryClientProvider>,
  )
}

function mockRecords(rows: unknown[], onRequest?: (url: URL) => void) {
  mswServer.use(
    http.get('*/api/v1/bases/:base_id/records', ({ request }) => {
      const url = new URL(request.url)
      onRequest?.(url)
      const rawCursor = url.searchParams.get('cursor') ?? ''
      const offset = Number.parseInt(rawCursor.replace(/^cur_/, ''), 10) || 0
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
}

function recordRow(id: string, cellsMap: Record<string, unknown>) {
  return {
    id,
    table_id: 'tbl_1',
    cells_map: cellsMap,
    created_by: 'usr_1',
    created_at: '1730000000000',
    version: '1',
  }
}

describe('记录游标加载（H-48）', () => {
  it('首屏 20 条；滚动到已加载末尾自动按 next_cursor 取下一页，后续 limit=100', async () => {
    const rows = Array.from({ length: 25 }, (_, index) =>
      recordRow(`rec_${index + 1}`, {
        fld_title: { string_value: `记录 ${index + 1}` },
      }),
    )
    const requested: URL[] = []
    mockRecords(rows, (url) => requested.push(url))
    const onRecordCountChange = vi.fn()
    renderGrid({ props: { onRecordCountChange } })

    expect(await screen.findByText('记录 20')).toBeInTheDocument()
    await waitFor(() => expect(onRecordCountChange).toHaveBeenCalledWith(20, true))
    expect(requested[0]?.searchParams.get('limit')).toBe('20')
    expect(requested[0]?.searchParams.get('cursor')).toBeNull()
    expect(requested[0]?.searchParams.get('table_id')).toBe('tbl_1')
    expect(requested[0]?.searchParams.get('view_id')).toBe('view_1')

    const viewport = screen.getByRole('grid', { name: '记录网格' })
    viewport.scrollTop = 400
    fireEvent.scroll(viewport)

    await waitFor(() => expect(requested).toHaveLength(2))
    expect(requested[1]?.searchParams.get('cursor')).toBe('cur_20')
    expect(requested[1]?.searchParams.get('limit')).toBe('100')
    expect(await screen.findByText('记录 25')).toBeInTheDocument()
    await waitFor(() => expect(onRecordCountChange).toHaveBeenCalledWith(25, false))
  })
})

describe('11 类单元格渲染（真实 H-48 protojson 包装）', () => {
  const DATE_VALUE = new Date(2024, 0, 5).getTime()
  const FIELDS: RawField[] = [
    rawField('fld_title', '标题', 'text', { is_primary: true, position: 0 }),
    rawField('fld_num', '金额', 'number', {
      position: 1,
      config_map: { format: 'currency', currency: '¥', decimals: 2, thousands: true },
    }),
    rawField('fld_sel', '状态', 'select', {
      position: 2,
      config_map: {
        option_list: [
          { id: 'opt_1', name: '进行中', color: 'blue' },
          { id: 'opt_2', name: '完成', color: 'green' },
        ],
        allow_create: false,
      },
    }),
    rawField('fld_multi', '标签', 'multiselect', {
      position: 3,
      config_map: {
        option_list: [
          { id: 'opt_1', name: '进行中', color: 'blue' },
          { id: 'opt_2', name: '完成', color: 'green' },
        ],
      },
    }),
    rawField('fld_date', '截止日期', 'date', {
      position: 4,
      config_map: { include_time: false, format: 'YYYY-MM-DD' },
    }),
    rawField('fld_person', '负责人', 'person', {
      position: 5,
      config_map: { allow_multiple: false },
    }),
    rawField('fld_file', '附件', 'attachment', { position: 6, config_map: {} }),
    rawField('fld_url', '官网', 'url', { position: 7, config_map: {} }),
    rawField('fld_formula', '合计', 'formula', {
      position: 8,
      config_map: { expression: 'SUM({金额})' },
    }),
    rawField('fld_relation', '客户', 'relation', {
      position: 9,
      config_map: { target_table_id: 'tbl_2', allow_multiple: false },
    }),
    rawField('fld_lookup', '客户名引用', 'lookup', {
      position: 10,
      config_map: { relation_field_id: 'fld_relation', target_field_id: 'fld_name' },
    }),
  ]

  function mockMetadata() {
    const requested: URL[] = []
    mswServer.use(
      http.get('*/api/v1/bases/:base_id/shares/members', () =>
        ok({
          collaborator_list: [
            { user_id: 'usr_1', username: 'zhangsan', display_name: '张三', role: 'ROLE_EDITOR' },
          ],
        }),
      ),
      http.get('*/api/v1/tables/:table_id/fields', ({ params }) =>
        ok({
          field_list:
            String(params.table_id) === 'tbl_2'
              ? [
                  rawField('fld_name', '客户名', 'text', {
                    table_id: 'tbl_2',
                    is_primary: true,
                  }),
                ]
              : FIELDS,
        }),
      ),
      http.get('*/api/v1/tables/:table_id/views', ({ params }) =>
        ok({
          view_list:
            String(params.table_id) === 'tbl_2'
              ? [{ id: 'view_2', table_id: 'tbl_2', name: '默认视图', type: 'table', created_at: '0' }]
              : [],
        }),
      ),
      http.get('*/api/v1/bases/:base_id/records', ({ request }) => {
        const url = new URL(request.url)
        requested.push(url)
        if (url.searchParams.get('table_id') === 'tbl_2') {
          return ok({
            record_list: [
              {
                id: 'rec_9',
                table_id: 'tbl_2',
                cells_map: { fld_name: { string_value: '客户甲' } },
                created_by: 'usr_1',
                created_at: '0',
                version: '1',
              },
            ],
            has_more: false,
            next_cursor: null,
          })
        }
        return ok({
          record_list: [
            recordRow('rec_1', {
              fld_title: { string_value: '任务说明' },
              fld_num: { int_value: '0' },
              fld_sel: { string_value: 'opt_1' },
              fld_multi: {
                list_value: { value_list: [{ string_value: 'opt_1' }, { string_value: 'opt_2' }] },
              },
              fld_date: { date_value_ms: String(DATE_VALUE) },
              fld_person: { string_value: 'usr_1' },
              fld_file: { list_value: { value_list: [{ string_value: 'fil_1' }] } },
              fld_url: {
                string_value: '{"url":"https://a.com","display_text":"官网"}',
              },
              fld_formula: { string_value: '#ERROR:除数为空' },
              fld_relation: { string_value: 'rec_9' },
              fld_lookup: { int_value: '126' },
            }),
          ],
          has_more: false,
          next_cursor: null,
        })
      }),
    )
    return requested
  }

  it('11 类字段按各自规则渲染，关联点击回调目标表与记录', async () => {
    const requested = mockMetadata()
    const onOpenRelation = vi.fn()
    renderGrid({ fields: FIELDS, props: { onOpenRelation } })

    const textCell = await screen.findByTestId('cell-rec_1-fld_title')
    expect(within(textCell).getByText('任务说明')).toHaveAttribute('title', '任务说明')

    expect(within(screen.getByTestId('cell-rec_1-fld_num')).getByText('¥0.00')).toBeInTheDocument()
    expect(within(screen.getByTestId('cell-rec_1-fld_sel')).getByText('进行中')).toBeInTheDocument()
    expect(within(screen.getByTestId('cell-rec_1-fld_multi')).getByText('进行中')).toBeInTheDocument()
    expect(within(screen.getByTestId('cell-rec_1-fld_multi')).getByText('完成')).toBeInTheDocument()
    expect(within(screen.getByTestId('cell-rec_1-fld_date')).getByText('2024-01-05')).toBeInTheDocument()
    expect(within(screen.getByTestId('cell-rec_1-fld_person')).getByText('张三')).toBeInTheDocument()
    expect(screen.getByText('fil_1')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'fil_1' })).toHaveAttribute(
      'src',
      '/api/v1/files/fil_1/thumb',
    )

    const link = screen.getByRole('link', { name: '官网' })
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')

    const formula = within(screen.getByTestId('cell-rec_1-fld_formula')).getByText('#ERROR')
    expect(formula).toHaveAttribute('title', '除数为空')
    expect(within(screen.getByTestId('cell-rec_1-fld_lookup')).getByText('126')).toBeInTheDocument()

    await waitFor(() =>
      expect(within(screen.getByTestId('cell-rec_1-fld_relation')).getByText('客户甲')).toBeInTheDocument(),
    )
    fireEvent.click(within(screen.getByTestId('cell-rec_1-fld_relation')).getByText('客户甲'))
    expect(onOpenRelation).toHaveBeenCalledWith('tbl_2', 'rec_9')

    const targetRequest = requested.find((url) => url.searchParams.get('table_id') === 'tbl_2')
    expect(targetRequest?.searchParams.get('limit')).toBe('100')
    expect(targetRequest?.searchParams.get('view_id')).toBe('view_2')
  })

  it('会话内 fileMetaMap 并入附件渲染配置，网格即时显示文件名（票 08）', async () => {
    workspaceStore.setState({
      fileMetaMap: { fil_1: { name: '方案.pdf', displayName: '方案.pdf' } },
    })
    mockMetadata()
    renderGrid({ fields: FIELDS })

    const cell = await screen.findByTestId('cell-rec_1-fld_file')
    expect(within(cell).getByText('方案.pdf')).toBeInTheDocument()
    expect(within(cell).getByRole('img', { name: '方案.pdf' })).toHaveAttribute(
      'src',
      '/api/v1/files/fil_1/thumb',
    )
    workspaceStore.setState({ fileMetaMap: {} })
  })

  it('关联目标表元数据失败时降级显示 record_id', async () => {
    mswServer.use(
      http.get('*/api/v1/tables/:table_id/fields', () => fail(9001)),
      http.get('*/api/v1/tables/:table_id/views', () =>
        ok({ view_list: [{ id: 'view_2', table_id: 'tbl_2', name: '默认视图', type: 'table', created_at: '0' }] }),
      ),
      http.get('*/api/v1/bases/:base_id/records', () =>
        ok({
          record_list: [recordRow('rec_1', { fld_relation: { string_value: 'rec_9' } })],
          has_more: false,
          next_cursor: null,
        }),
      ),
    )
    renderGrid({
      fields: [
        rawField('fld_relation', '客户', 'relation', {
          config_map: { target_table_id: 'tbl_2', allow_multiple: false },
        }),
      ],
      props: { onOpenRelation: vi.fn() },
    })

    const cell = await screen.findByTestId('cell-rec_1-fld_relation')
    expect(within(cell).getByText('rec_9')).toBeInTheDocument()
  })

  it('人员元数据（H-63）失败时降级显示 user_id', async () => {
    mswServer.use(
      http.get('*/api/v1/bases/:base_id/shares/members', () => fail(9001)),
      http.get('*/api/v1/bases/:base_id/records', () =>
        ok({
          record_list: [recordRow('rec_1', { fld_person: { string_value: 'usr_1' } })],
          has_more: false,
          next_cursor: null,
        }),
      ),
    )
    renderGrid({ fields: [rawField('fld_person', '负责人', 'person')] })

    const cell = await screen.findByTestId('cell-rec_1-fld_person')
    expect(within(cell).getByText('usr_1')).toBeInTheDocument()
  })
})

describe('空态与错误态（票 05 + 3.8.3）', () => {
  it('空表（有字段无记录）呈「从第一行开始」', async () => {
    mockRecords([])
    renderGrid()

    expect(await screen.findByText('从第一行开始')).toBeInTheDocument()
  })

  it('筛选无结果呈约定空态与清除筛选入口', async () => {
    mockRecords([])
    const onClearFilters = vi.fn()
    renderGrid({ props: { hasActiveFilters: true, onClearFilters } })

    expect(await screen.findByText('没有符合筛选条件的记录')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '清除筛选' }))
    expect(onClearFilters).toHaveBeenCalledTimes(1)
  })

  it('全部字段被隐藏时呈提示与恢复入口', () => {
    const onRestoreFields = vi.fn()
    renderGrid({ props: { allFieldsHidden: true, onRestoreFields } })

    expect(screen.getByText('所有字段均已隐藏')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '恢复字段显示' }))
    expect(onRestoreFields).toHaveBeenCalledTimes(1)
  })

  it('请求失败展示错误文案与重试入口，重试再次请求', async () => {
    let calls = 0
    mswServer.use(
      http.get('*/api/v1/bases/:base_id/records', () => {
        calls += 1
        return calls === 1 ? fail(9001) : ok({ record_list: [], has_more: false, next_cursor: null })
      }),
    )
    renderGrid()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('服务暂时不可用')
    fireEvent.click(within(alert).getByRole('button', { name: '重新加载记录' }))

    await waitFor(() => expect(calls).toBe(2))
    expect(await screen.findByText('从第一行开始')).toBeInTheDocument()
  })

  it('无字段时提示先新增字段', () => {
    renderGrid({ fields: [], props: { baseId: undefined } })

    expect(screen.getByText('请先新增字段')).toBeInTheDocument()
  })
})
