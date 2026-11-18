// 单元格提交埋点（工单 17 / 附录 C）：提交保存成功上报 fe_cell_edit_submit，携带 field_type；
// 提交失败（网络/冲突/容量）不上报。埋点 sender 注入 spy（batchSize=1 即刻 flush）。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { mswServer } from '@/mocks/server'
import { ManualClock } from '@/test/manual-clock'
import { resetTracker, type TrackEvent } from '@/lib/tracker'
import { ToastProvider } from '@/ui-kit'

import type { FieldRecord } from '../api/field'
import { GridArea, type GridAreaProps } from './GridArea'

function ok(data: unknown) {
  return HttpResponse.json({ code: 0, message: 'ok', data })
}

const FIELDS: FieldRecord[] = [
  {
    id: 'fld_title',
    tableId: 'tbl_1',
    name: '标题',
    type: 'text',
    configMap: {},
    isPrimary: true,
    position: 0,
    description: '',
  },
  {
    id: 'fld_num',
    tableId: 'tbl_1',
    name: '金额',
    type: 'number',
    configMap: {},
    isPrimary: false,
    position: 1,
    description: '',
  },
]

const ROWS = [
  {
    id: 'rec_1',
    table_id: 'tbl_1',
    cells_map: { fld_title: { string_value: '任务一' }, fld_num: { double_value: 1 } },
    created_by: 'usr_1',
    created_at: '0',
    version: '1',
  },
]

let batches: TrackEvent[][] = []
let clock: ManualClock

function installTrackingSpy(): void {
  batches = []
  const sender = async (events: readonly TrackEvent[]): Promise<void> => {
    batches.push([...events])
  }
  resetTracker({ sender, scheduler: clock, batchSize: 1 })
}

beforeEach(() => {
  clock = new ManualClock()
  installTrackingSpy()
  mswServer.use(
    http.get('*/api/v1/bases/:base_id/records', () =>
      ok({ record_list: ROWS, has_more: false, next_cursor: null }),
    ),
    http.get('*/api/v1/bases/:base_id/shares/person-directory', () =>
      ok({ collaborator_list: [] }),
    ),
    http.get('*/api/v1/tables/:table_id/fields', () => ok({ field_list: [] })),
    http.get('*/api/v1/tables/:table_id/views', () =>
      ok({ view_list: [{ id: 'view_1', table_id: 'tbl_1', name: '默认', type: 'table', created_at: '0' }] }),
    ),
  )
})

afterEach(async () => {
  await resetTracker()
})

function renderGrid(props: Partial<GridAreaProps> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <GridArea
          tableId="tbl_1"
          baseId="base_1"
          viewId="view_1"
          fields={FIELDS}
          canManageStructure={false}
          canEditFields={false}
          {...props}
        />
      </ToastProvider>
    </QueryClientProvider>,
  )
}

async function submitTitle(newValue: string): Promise<void> {
  const cell = await screen.findByTestId('cell-rec_1-fld_title')
  fireEvent.doubleClick(cell)
  fireEvent.change(screen.getByRole('textbox'), { target: { value: newValue } })
  fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
}

describe('单元格提交埋点（fe_cell_edit_submit）', () => {
  it('提交成功上报一次，field_type=text', async () => {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        ok({ value: { string_value: '新值' }, version: '2' }),
      ),
    )
    renderGrid()

    await submitTitle('新值')

    await waitFor(() => expect(batches).toHaveLength(1))
    const event = batches[0][0]
    expect(event.event_name).toBe('fe_cell_edit_submit')
    expect(event.detail).toEqual({ field_type: 'text' })
    await waitFor(() =>
      expect(screen.queryByTestId('editor-rec_1-fld_title')).not.toBeInTheDocument(),
    )
  })

  it('提交失败（网络错误）不上报', async () => {
    mswServer.use(
      http.put('*/api/v1/records/:record_id/cells/:field_id', () =>
        HttpResponse.json({ code: 9001, message: '服务暂不可用', data: null }, { status: 500 }),
      ),
    )
    renderGrid()

    await submitTitle('新值')

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(batches).toHaveLength(0)
  })
})
