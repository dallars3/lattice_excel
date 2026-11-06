// 建 Base 埋点（工单 17 / 附录 C）：新建 Base 成功上报 fe_base_create；失败不上报。
// 埋点 sender 注入 spy（batchSize=1 即刻 flush），MSW 只承担业务边界。
import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { ManualClock } from '@/test/manual-clock'
import { resetTracker, type TrackEvent } from '@/lib/tracker'
import { authStore } from '@/stores'

const T1 = new Date(2026, 10, 5, 10, 30).getTime()

function rawBase(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: 'base_new',
    name: '未命名多维表格',
    table_count: 0,
    my_role: 'owner',
    starred: false,
    last_modified_at: T1,
    last_modifier: 'Alice',
    ...overrides,
  }
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
  mswServer.use(mockEnvelope({ path: '/api/v1/notifications/unread-count', data: {} }))
})

afterEach(async () => {
  await resetTracker()
})

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

describe('建 Base 埋点（fe_base_create）', () => {
  it('新建 Base 成功上报一次，detail 为空对象', async () => {
    const home = {
      recent_list: [],
      created_list: [] as Record<string, unknown>[],
      shared_list: [],
      star_list: [],
      invitation_list: [],
    }
    mswServer.use(
      mockEnvelope({ path: '/api/v1/home', data: home }),
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/tables',
        data: {
          table_list: [
            {
              id: 'tbl_1',
              base_id: 'base_new',
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
      mockEnvelope({ path: '*/api/v1/views/:view_id/config', data: { view_config: {} } }),
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
        method: 'post',
        data: { record: { id: 'rec_1', cells_map: {}, version: 1 } },
      }),
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/records',
        data: {
          record_list: [],
          has_more: false,
          next_cursor: '',
          unsolved_comment_count_map: {},
        },
      }),
      mockEnvelope({
        path: '/api/v1/bases',
        method: 'post',
        onRequest: async () => {
          home.created_list.push(rawBase())
        },
        data: { base: rawBase() },
      }),
    )
    renderHome()

    fireEvent.click(await screen.findByRole('button', { name: '新建多维表格' }))

    await waitFor(() => expect(batches).toHaveLength(1))
    const event = batches[0][0]
    expect(event.event_name).toBe('fe_base_create')
    expect(event.detail).toEqual({})
  })

  it('5001 容量上限失败不上报', async () => {
    mswServer.use(
      mockEnvelope({ path: '/api/v1/home', data: { recent_list: [], created_list: [], shared_list: [], star_list: [], invitation_list: [] } }),
      mockEnvelope({ path: '/api/v1/bases', method: 'post', code: 5001 }),
    )
    renderHome()

    fireEvent.click(await screen.findByRole('button', { name: '新建多维表格' }))

    await waitFor(() =>
      expect(screen.getByRole('alert')).toBeInTheDocument(),
    )
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(batches).toHaveLength(0)
  })
})
