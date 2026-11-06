// 首屏可交互埋点（工单 17 / 附录 C / AC-NFR-01）：结构与首窗口记录就绪后上报一次
// fe_perf_screen_ready，携带 rows（首窗口行数）与 t_ms（自应用启动耗时）；会话内只报一次。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { ManualClock } from '@/test/manual-clock'
import { resetTracker, type TrackEvent } from '@/lib/tracker'
import { authStore } from '@/stores'

const BASE_ID = 'base_1'

function ok(data: unknown) {
  return { code: 0, message: 'ok', data }
}

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
  authStore.getState().clear()
  authStore.getState().setSession({
    user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
    enterprise: { id: 'ent_1', name: '启航科技' },
  })
  installTrackingSpy()
  mswServer.use(
    mockEnvelope({ path: '/api/v1/notifications/unread-count', data: {} }),
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
      path: '*/api/v1/views/:view_id/config',
      data: { view_config: {} },
    }),
  )
})

afterEach(async () => {
  await resetTracker()
})

function mockDomain(rows: Record<string, unknown>[]) {
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
            my_role: 'owner',
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
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/tables',
      data: {
        table_list: [
          {
            id: 'tbl_1',
            base_id: BASE_ID,
            name: '项目排期',
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
          { id: 'view_1', table_id: 'tbl_1', name: '表格视图', type: 'table', created_at: '0' },
        ],
      },
    }),
    mockEnvelope({
      path: '*/api/v1/bases/:base_id/records',
      data: { record_list: rows, has_more: false, next_cursor: null },
    }),
  )
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
}

function row(id: string, title: string): Record<string, unknown> {
  return {
    id,
    table_id: 'tbl_1',
    cells_map: { fld_primary: { string_value: title } },
    created_by: 'usr_1',
    created_at: '0',
    version: '1',
  }
}

describe('首屏可交互埋点（fe_perf_screen_ready）', () => {
  it('结构与首窗口记录就绪后上报一次，携带 rows 与 t_ms', async () => {
    mockDomain([row('rec_1', '任务一'), row('rec_2', '任务二')])
    renderWorkspace()

    await waitFor(() => expect(screen.getByText('任务一')).toBeInTheDocument())
    await waitFor(() => expect(batches).toHaveLength(1))

    const event = batches[0][0]
    expect(event.event_name).toBe('fe_perf_screen_ready')
    expect(event.detail.rows).toBe(2)
    expect(event.detail.t_ms).toBeTypeOf('number')
    expect(event.detail.t_ms).toBeGreaterThanOrEqual(0)
  })

  it('空表首屏同样上报（rows=0），会话内不重复上报', async () => {
    mockDomain([])
    renderWorkspace()

    await waitFor(() => expect(batches).toHaveLength(1))
    expect(batches[0][0].detail.rows).toBe(0)

    await waitFor(() => expect(screen.getByRole('region', { name: '网格' })).toBeInTheDocument())
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(batches).toHaveLength(1)
  })

  it('记录未就绪（记录接口失败）不上报', async () => {
    mswServer.use(
      mockEnvelope({ path: '/api/v1/home', data: ok({ recent_list: [], created_list: [], shared_list: [], star_list: [], invitation_list: [] }) }),
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/tables',
        data: ok({
          table_list: [
            {
              id: 'tbl_1',
              base_id: BASE_ID,
              name: '项目排期',
              field_count: 1,
              view_count: 1,
              created_at: '1730000000000',
            },
          ],
        }),
      }),
      mockEnvelope({
        path: '*/api/v1/tables/:table_id/views',
        data: ok({
          view_list: [
            { id: 'view_1', table_id: 'tbl_1', name: '表格视图', type: 'table', created_at: '0' },
          ],
        }),
      }),
      mockEnvelope({ path: '*/api/v1/bases/:base_id/records', code: 9001 }),
    )
    renderWorkspace()

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(batches).toHaveLength(0)
  })
})
