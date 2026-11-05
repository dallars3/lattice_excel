import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import {
  fetchMe,
  loginAccount,
  loginAndLoadMe,
  logoutAccount,
  registerAccount,
  registerAndSignIn,
} from './api'

const USER = {
  id: 'usr_1',
  username: 'alice',
  display_name: 'Alice',
  enterprise_id: 'ent_1',
  created_at: '1730000000',
}

const ME = {
  user: USER,
  enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 3 },
  my_role: 'owner',
}

describe('auth api（H-01~H-04，后端严格 JSON 不接收 client_request_id）', () => {
  it('H-01 register：写调用不带 client_request_id；可选字段缺省不发送', async () => {
    const bodies: Record<string, unknown>[] = []
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/register',
        method: 'post',
        onRequest: async (request) => bodies.push((await request.json()) as Record<string, unknown>),
        data: { token: 'sess_1', user: USER },
      }),
    )
    await registerAccount({ username: 'alice', password: 'Pass1234' })
    expect(Object.keys(bodies[0]).sort()).toEqual(['password', 'username'])
  })

  it('H-01 register：建企业路径带 enterprise_name，链接路径带 link_token', async () => {
    const bodies: Record<string, unknown>[] = []
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/register',
        method: 'post',
        onRequest: async (request) => bodies.push((await request.json()) as Record<string, unknown>),
        data: { token: 'sess_1', user: USER },
      }),
    )
    await registerAccount({ username: 'bob', password: 'Pass1234', enterprise_name: '启航科技' })
    await registerAccount({ username: 'carol', password: 'Pass1234', link_token: 'lnk_01J' })
    expect(bodies[0].enterprise_name).toBe('启航科技')
    expect(bodies[0].link_token).toBeUndefined()
    expect(bodies[1].link_token).toBe('lnk_01J')
    expect(bodies[1].enterprise_name).toBeUndefined()
  })

  it('H-02 login：写调用不带 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/login',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { token: 'sess_1', user: USER },
      }),
    )
    await loginAccount({ username: 'alice', password: 'Pass1234' })
    expect(Object.keys(body).sort()).toEqual(['password', 'username'])
  })

  it('H-03 logout：写调用不带 client_request_id', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/logout',
        method: 'post',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: {},
      }),
    )
    await logoutAccount()
    expect(rawBody).not.toContain('client_request_id')
  })

  it('H-04 me：返回后端 protojson 形状（snake_case / int64 字符串）', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/auth/me', data: ME }))
    const me = await fetchMe()
    expect(me.user.username).toBe('alice')
    expect(me.user.created_at).toBe('1730000000')
    expect(me.enterprise?.name).toBe('启航科技')
    expect(me.my_role).toBe('owner')
  })

  it('registerAndSignIn：注册后补 H-02 登录换 Cookie，再以 H-04 me 为身份事实源', async () => {
    const calls: string[] = []
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/register',
        method: 'post',
        onRequest: () => calls.push('register'),
        data: { token: 'sess_register', user: USER },
      }),
      mockEnvelope({
        path: '/api/v1/auth/login',
        method: 'post',
        onRequest: () => calls.push('login'),
        data: { token: 'sess_login', user: USER },
      }),
      mockEnvelope({ path: '/api/v1/auth/me', onRequest: () => calls.push('me'), data: ME }),
    )
    const me = await registerAndSignIn({ username: 'alice', password: 'Pass1234' })
    expect(calls).toEqual(['register', 'login', 'me'])
    expect(me.user.username).toBe('alice')
  })

  it('loginAndLoadMe：登录成功后拉 me 作为会话事实源', async () => {
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/login',
        method: 'post',
        data: { token: 'sess_login', user: USER },
      }),
      mockEnvelope({ path: '/api/v1/auth/me', data: ME }),
    )
    const me = await loginAndLoadMe({ username: 'alice', password: 'Pass1234' })
    expect(me.my_role).toBe('owner')
  })
})
