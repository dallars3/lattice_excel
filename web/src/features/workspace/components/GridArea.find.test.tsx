// 网格视图内搜索测试（缺陷 #29）：Ctrl+F 呼出搜索条，对已加载数据过滤+高亮，
// Esc 关闭；全程零服务端视图配置写请求（H-47 canary）。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { mswServer } from '@/mocks/server'
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

interface RenderOptions {
  fields?: RawField[]
  props?: Partial<GridAreaProps>
}

function mockRecords(rows: unknown[]) {
  mswServer.use(
    http.get('*/api/v1/bases/:base_id/records', () =>
      ok({ record_list: rows, has_more: false, next_cursor: null }),
    ),
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

function visibleRecordIds(): string[] {
  return screen
    .getAllByRole('row')
    .map((row) => row.getAttribute('data-record-id'))
    .filter((id): id is string => id !== null)
}

/** 网格在 jsdom 无焦点态也可派发 Ctrl+F：与 Playwright evaluate 同构的合成事件。 */
function pressCtrlF() {
  fireEvent.keyDown(screen.getByRole('grid', { name: '记录网格' }), {
    key: 'f',
    ctrlKey: true,
  })
}

describe('视图内搜索（缺陷 #29）', () => {
  it('Ctrl+F 打开搜索条；无单元格焦点也可呼出', async () => {
    mockRecords([
      recordRow('rec_1', { fld_title: { string_value: '记录甲' } }),
      recordRow('rec_2', { fld_title: { string_value: '记录乙' } }),
    ])
    renderGrid()
    expect(await screen.findByText('记录甲')).toBeInTheDocument()

    pressCtrlF()

    const bar = await screen.findByTestId('grid-find-bar')
    expect(bar).toBeInTheDocument()
    expect(screen.getByTestId('grid-find-input')).toHaveFocus()
  })

  it('输入关键词过滤已加载行并高亮命中单元格，显示匹配计数', async () => {
    mockRecords([
      recordRow('rec_1', { fld_title: { string_value: '苹果清单' } }),
      recordRow('rec_2', { fld_title: { string_value: '香蕉清单' } }),
      recordRow('rec_3', { fld_title: { string_value: '苹果汇总' } }),
    ])
    renderGrid({
      fields: [rawField('fld_title', '标题', 'text', { is_primary: true })],
    })
    expect(await screen.findByText('香蕉清单')).toBeInTheDocument()

    pressCtrlF()
    fireEvent.change(screen.getByTestId('grid-find-input'), { target: { value: '苹果' } })

    await waitFor(() => expect(visibleRecordIds()).toEqual(['rec_1', 'rec_3']))
    expect(screen.getByTestId('grid-find-count')).toHaveTextContent('2/3')
    const hit = screen.getByTestId('cell-rec_1-fld_title')
    expect(hit).toHaveAttribute('data-cell-match', 'true')
    expect(screen.queryByTestId('cell-rec_2-fld_title')).not.toBeInTheDocument()
  })

  it('Esc 关闭搜索条并恢复全部已加载行', async () => {
    mockRecords([
      recordRow('rec_1', { fld_title: { string_value: '苹果清单' } }),
      recordRow('rec_2', { fld_title: { string_value: '香蕉清单' } }),
    ])
    renderGrid()
    expect(await screen.findByText('香蕉清单')).toBeInTheDocument()

    pressCtrlF()
    fireEvent.change(screen.getByTestId('grid-find-input'), { target: { value: '苹果' } })
    await waitFor(() => expect(visibleRecordIds()).toEqual(['rec_1']))

    fireEvent.keyDown(screen.getByTestId('grid-find-input'), { key: 'Escape' })

    await waitFor(() => expect(visibleRecordIds()).toEqual(['rec_1', 'rec_2']))
    expect(screen.queryByTestId('grid-find-bar')).not.toBeInTheDocument()
  })

  it('搜索全程不发出视图配置写请求（纯客户端，不改服务端视图配置）', async () => {
    const viewConfigWrites = vi.fn()
    mswServer.use(
      http.put('*/api/v1/views/:view_id/config', ({ request }) => {
        viewConfigWrites(new URL(request.url).pathname)
        return ok({})
      }),
      http.patch('*/api/v1/views/:view_id', ({ request }) => {
        viewConfigWrites(new URL(request.url).pathname)
        return ok({})
      }),
    )
    mockRecords([
      recordRow('rec_1', { fld_title: { string_value: '苹果清单' } }),
      recordRow('rec_2', { fld_title: { string_value: '香蕉清单' } }),
    ])
    renderGrid()
    expect(await screen.findByText('苹果清单')).toBeInTheDocument()

    pressCtrlF()
    fireEvent.change(screen.getByTestId('grid-find-input'), { target: { value: '苹果' } })
    await waitFor(() => expect(visibleRecordIds()).toEqual(['rec_1']))
    fireEvent.keyDown(screen.getByTestId('grid-find-input'), { key: 'Escape' })
    await waitFor(() => expect(visibleRecordIds()).toEqual(['rec_1', 'rec_2']))

    expect(viewConfigWrites).not.toHaveBeenCalled()
  })
})
