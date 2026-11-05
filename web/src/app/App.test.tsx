import '@testing-library/jest-dom/vitest'

import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { DEFAULT_NOTICE_TEXT } from '@/lib/env'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore } from '@/stores'

import { AppProviders, appQueryClient } from './providers'
import { AppRoutes } from './router'

const ME = {
  user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
  enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  my_role: 'owner',
}

// 票 01 冒烟用例（票 02 调整渲染位置）：App.tsx 改为路由出口后，标题+声明文案并入 /home 首页路由，
// 断言保持不变；票 01 起 /home 经 RequireSession 守卫，需在 HTTP 边界 mock H-04 me。
describe('App 占位首页', () => {
  beforeEach(() => {
    authStore.getState().clear()
    appQueryClient.clear()
  })

  it('渲染产品标题与试运行声明文案', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/auth/me', data: ME }))
    render(
      <AppProviders>
        <MemoryRouter initialEntries={['/home']}>
          <AppRoutes />
        </MemoryRouter>
      </AppProviders>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )
    expect(screen.getByText(DEFAULT_NOTICE_TEXT)).toBeInTheDocument()
  })
})
