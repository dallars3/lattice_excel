// 登录埋点（工单 17 / 附录 C）：登录成功上报 fe_auth_login_success，携带 user_type。
// 埋点 sender 注入 spy（batchSize=1 即刻 flush），MSW 只承担登录/me 业务边界。
import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { ManualClock } from '@/test/manual-clock'

import { resetTracker, type TrackEvent } from '@/lib/tracker'

import { LoginForm } from './LoginForm'

const INTERNAL_ME = {
  user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
  enterprise: { id: 'ent_1', name: '启航科技' },
  my_role: 'owner',
}

const EXTERNAL_ME = {
  user: { id: 'usr_2', username: 'guest', display_name: '访客' },
  my_role: '',
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

function mockLogin(me: unknown): void {
  mswServer.use(
    mockEnvelope({ path: '/api/v1/auth/login', method: 'post', data: { token: 'sess_1', user: {} } }),
    mockEnvelope({ path: '/api/v1/auth/me', data: me }),
  )
}

async function submitLogin(): Promise<void> {
  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'alice' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'Pass1234' } })
  fireEvent.click(screen.getByRole('button', { name: '登录' }))
  await waitFor(() => expect(batches).toHaveLength(1))
}

beforeEach(() => {
  clock = new ManualClock()
  installTrackingSpy()
})

afterEach(async () => {
  await resetTracker()
})

describe('登录埋点（fe_auth_login_success）', () => {
  it('内部用户登录成功：user_type=internal', async () => {
    mockLogin(INTERNAL_ME)
    render(<LoginForm />)
    await submitLogin()

    const event = batches[0][0]
    expect(event.event_name).toBe('fe_auth_login_success')
    expect(event.detail).toEqual({ user_type: 'internal' })
    expect(event.event_id.length).toBeGreaterThanOrEqual(8)
    expect(event.client_at_ms).toBe(0)
  })

  it('外部用户（无企业归属）登录成功：user_type=external', async () => {
    mockLogin(EXTERNAL_ME)
    render(<LoginForm />)
    await submitLogin()

    expect(batches[0][0].event_name).toBe('fe_auth_login_success')
    expect(batches[0][0].detail).toEqual({ user_type: 'external' })
  })

  it('登录失败不上报', async () => {
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/login',
        method: 'post',
        code: 2002,
        message: '用户名或密码错误',
      }),
    )
    render(<LoginForm />)
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'wrong' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('用户名或密码错误'))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(batches).toHaveLength(0)
  })
})
