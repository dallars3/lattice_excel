import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppProviders, appQueryClient } from '@/app/providers'
import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore } from '@/stores'

import { RequireSession } from './RequireSession'

const ME = {
  user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
  enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  my_role: 'owner',
}

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location-pattern">{location.pathname + location.search}</div>
}

function renderGuard(entry: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route
            path="/base/:id"
            element={
              <RequireSession>
                <div>受保护的 Base 页面</div>
              </RequireSession>
            }
          />
          <Route path="/login" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('RequireSession 会话守卫（票 01）', () => {
  beforeEach(() => {
    authStore.getState().clear()
    appQueryClient.clear()
  })

  it('无会话且 me 返回 2001：跳 /login?next=<当前 pathname+search>', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/auth/me', code: 2001 }))
    renderGuard('/base/base_001?view=vw_1')

    await waitFor(() =>
      expect(screen.getByTestId('location-pattern')).toHaveTextContent(
        '/login?next=%2Fbase%2Fbase_001%3Fview%3Dvw_1',
      ),
    )
  })

  it('首屏 me 返回 2001：跳登录页且不叠加重登弹窗，登录表单可用', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/auth/me', code: 2001 }))
    render(
      <AppProviders>
        <MemoryRouter initialEntries={['/base/base_001']}>
          <AppRoutes />
        </MemoryRouter>
      </AppProviders>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('登录'),
    )
    // 全局桥会先打开重登弹窗；RequireSession 跳登录前已关闭，不再与页面登录表单叠加。
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByLabelText('用户名')).toBeInTheDocument()
    expect(screen.getByLabelText('密码')).toBeInTheDocument()
    expect(authStore.getState().reloginOpen).toBe(false)
  })

  it('me 成功：渲染受保护内容并以 me 写入会话（internal 派生）', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/auth/me', data: ME }))
    renderGuard('/base/base_001')

    await waitFor(() => expect(screen.getByText('受保护的 Base 页面')).toBeInTheDocument())
    expect(authStore.getState().session?.user.username).toBe('alice')
    expect(authStore.getState().session?.internal).toBe(true)
  })

  it('已有会话：直接渲染，不再探测 me', async () => {
    let hits = 0
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/me',
        onRequest: () => {
          hits += 1
        },
        data: ME,
      }),
    )
    authStore.getState().setSession({ user: ME.user, enterprise: ME.enterprise })
    renderGuard('/base/base_001')

    expect(screen.getByText('受保护的 Base 页面')).toBeInTheDocument()
    expect(hits).toBe(0)
  })
})
