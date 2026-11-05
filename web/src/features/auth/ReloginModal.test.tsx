import '@testing-library/jest-dom/vitest'

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppProviders, appQueryClient } from '@/app/providers'
import { http } from '@/core/http'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { authKeys } from '@/query-keys'
import { authStore } from '@/stores'

import { ReloginModal } from './ReloginModal'

const ME = {
  user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
  enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  my_role: 'owner',
}

function mockSignInSuccess() {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/auth/login',
      method: 'post',
      data: { token: 'sess_1', user: { id: 'usr_1', username: 'stale-name' } },
    }),
    mockEnvelope({ path: '/api/v1/auth/me', data: ME }),
  )
}

describe('2001 全局重登弹窗（票 01）', () => {
  beforeEach(() => {
    authStore.getState().clear()
    appQueryClient.clear()
  })

  it('任意接口回包 2001 → 打开弹窗；重登成功 → 关闭并失效 me 查询', async () => {
    appQueryClient.setQueryData(authKeys.me(), ME)
    mockSignInSuccess()
    mswServer.use(mockEnvelope({ path: '/api/v1/probe', code: 2001 }))

    render(
      <AppProviders>
        <ReloginModal />
      </AppProviders>,
    )

    await act(async () => {
      await http.get('/api/v1/probe').catch(() => undefined)
    })
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('登录状态已失效')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'Pass1234' } })
    fireEvent.click(screen.getByRole('button', { name: '重新登录' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(authStore.getState().session?.user.username).toBe('alice')
    expect(appQueryClient.getQueryState(authKeys.me())?.isInvalidated).toBe(true)
  })

  it('非 2001 错误码不打开重登弹窗（保持原有消费语义）', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/probe', code: 1001 }))
    render(
      <AppProviders>
        <ReloginModal />
      </AppProviders>,
    )

    await act(async () => {
      await http.get('/api/v1/probe').catch(() => undefined)
    })
    expect(authStore.getState().reloginOpen).toBe(false)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
