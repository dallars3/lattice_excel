import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { AppProviders } from '@/app/providers'
import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authStore } from '@/stores'

const ME = {
  user: {
    id: 'usr_1',
    username: 'alice',
    display_name: 'Alice',
    enterprise_id: 'ent_1',
  },
  enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  my_role: 'owner',
}

function renderAt(entry: string) {
  return render(
    <AppProviders>
      <MemoryRouter initialEntries={[entry]}>
        <AppRoutes />
      </MemoryRouter>
    </AppProviders>,
  )
}

function fillCredentials(username = 'alice', password = 'Pass1234') {
  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: username } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: password } })
}

const HOME = {
  recent_list: [],
  created_list: [],
  shared_list: [],
  star_list: [],
  invitation_list: [],
}

function mockLoginSuccess() {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/auth/login',
      method: 'post',
      // 登录响应里的 user 不作为会话事实源；身份必须取 H-04 me。
      data: { token: 'sess_1', user: { id: 'usr_1', username: 'stale-name' } },
    }),
    mockEnvelope({ path: '/api/v1/auth/me', data: ME }),
    mockEnvelope({ path: '/api/v1/home', data: HOME }),
  )
}

describe('登录页（票 01）', () => {
  it('2002：显示锁定/失败提示与固定「如遗忘密码」说明，不提供找回入口', async () => {
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/login',
        method: 'post',
        code: 2002,
        message: '用户名或密码错误',
      }),
    )
    renderAt('/login')
    fillCredentials('alice', 'Wrong1234')
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() =>
      expect(
        screen.getByText('用户名或密码错误，或账号已锁定，请稍后再试'),
      ).toBeInTheDocument(),
    )
    expect(screen.getByText('如遗忘密码，账号将无法使用')).toBeInTheDocument()
    expect(screen.queryByText(/找回密码|忘记密码/)).not.toBeInTheDocument()
    expect(screen.getByLabelText('用户名')).toHaveValue('alice')
    expect(screen.getByLabelText('密码')).toHaveValue('Wrong1234')
  })

  it('成功写入 me 身份并跳转首页（无 next 时默认 /home）', async () => {
    authStore.getState().clear()
    mockLoginSuccess()
    renderAt('/login')
    fillCredentials()
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )
    expect(authStore.getState().session?.user.username).toBe('alice')
    expect(authStore.getState().session?.internal).toBe(true)
  })

  it('存在合法 next 时回到该路径（/base/base_001）', async () => {
    authStore.getState().clear()
    mockLoginSuccess()
    renderAt('/login?next=%2Fbase%2Fbase_001')
    fillCredentials()
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Base 工作台'),
    )
    expect(screen.getByText('base_001')).toBeInTheDocument()
  })

  it('next 为协议相对地址（//evil）时拒绝，回退 /home', async () => {
    authStore.getState().clear()
    mockLoginSuccess()
    renderAt('/login?next=%2F%2Fevil.example')
    fillCredentials()
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )
  })
})
