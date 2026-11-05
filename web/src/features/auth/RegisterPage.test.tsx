import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { AppProviders } from '@/app/providers'
import { AppRoutes } from '@/app/router'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import type { AuthMeData } from './types'

const USER = {
  id: 'usr_1',
  username: 'alice',
  display_name: 'Alice',
  enterprise_id: 'ent_1',
}
const ME = {
  user: USER,
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

/** 注册成功路径：H-01 → H-02 → H-04 三段 handler，并把请求体收集给断言。 */
function mockRegisterSuccess(bodies: Record<string, unknown>[], meData: AuthMeData = ME) {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/auth/register',
      method: 'post',
      onRequest: async (request) => bodies.push((await request.json()) as Record<string, unknown>),
      data: { token: 'sess_register', user: USER },
    }),
    mockEnvelope({
      path: '/api/v1/auth/login',
      method: 'post',
      data: { token: 'sess_login', user: USER },
    }),
    mockEnvelope({ path: '/api/v1/auth/me', data: meData }),
  )
}

describe('注册页三条归属路径（票 01）', () => {
  it('无参数：建企业文案 + 企业名称可选输入；body 带 enterprise_name、不带 link_token', async () => {
    const bodies: Record<string, unknown>[] = []
    mockRegisterSuccess(bodies)
    renderAt('/register')

    expect(screen.getByText(/注册即为你新建一个企业/)).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('企业名称（可选）'), {
      target: { value: '启航科技' },
    })
    fillCredentials()
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    await waitFor(() => expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'))
    expect(bodies[0].enterprise_name).toBe('启航科技')
    expect(bodies[0].link_token).toBeUndefined()
    expect(bodies[0].client_request_id).toBeUndefined()
  })

  it.each(['/register?invite=1', '/register?invite_id=inv_01J'])(
    '%s：邀请文案，企业名称输入不出现，body 无归属参数',
    async (entry) => {
      const bodies: Record<string, unknown>[] = []
      mockRegisterSuccess(bodies)
      renderAt(entry)

      expect(screen.getByText(/你将加入邀请你的企业/)).toBeInTheDocument()
      expect(screen.queryByLabelText('企业名称（可选）')).not.toBeInTheDocument()
      fillCredentials()
      fireEvent.click(screen.getByRole('button', { name: '注册' }))

      await waitFor(() =>
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
      )
      expect(Object.keys(bodies[0]).sort()).toEqual(['password', 'username'])
    },
  )

  it('/register?link_token=：链接文案，企业名称不出现，body 带 link_token', async () => {
    const bodies: Record<string, unknown>[] = []
    mockRegisterSuccess(bodies, { user: { ...USER, enterprise_id: '' }, my_role: 'editor' })
    renderAt('/register?link_token=lnk_01J')

    expect(screen.getByText(/你将凭分享链接获得对应表格的访问权限/)).toBeInTheDocument()
    expect(screen.queryByLabelText('企业名称（可选）')).not.toBeInTheDocument()
    fillCredentials()
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    await waitFor(() => expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'))
    expect(bodies[0].link_token).toBe('lnk_01J')
    expect(bodies[0].enterprise_name).toBeUndefined()
  })
})

describe('注册表单校验（票 01）', () => {
  it('用户名/密码非法时不发请求，字段级错误就地展示', () => {
    let hits = 0
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/register',
        method: 'post',
        onRequest: () => {
          hits += 1
        },
      }),
    )
    renderAt('/register')

    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: '1ab' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'abcdefgh' } })
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    expect(screen.getByText('用户名需为 4–20 位')).toBeInTheDocument()
    expect(screen.getByText('密码需同时包含字母与数字')).toBeInTheDocument()
    expect(hits).toBe(0)
  })

  it('企业名称非法（1 字）时同样不发请求', () => {
    let hits = 0
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/register',
        method: 'post',
        onRequest: () => {
          hits += 1
        },
      }),
    )
    renderAt('/register')

    fillCredentials()
    fireEvent.change(screen.getByLabelText('企业名称（可选）'), { target: { value: '启' } })
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    expect(screen.getByText('企业名称需为 2–30 个字符')).toBeInTheDocument()
    expect(hits).toBe(0)
  })
})

describe('注册服务端错误（票 01：1002/1001 行内错误且保留输入）', () => {
  it('1002 → 用户名行内「用户名已存在」，已填内容不丢失', async () => {
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/register',
        method: 'post',
        code: 1002,
        message: '用户名已存在',
      }),
    )
    renderAt('/register')
    fillCredentials('alice', 'Pass1234')
    fireEvent.change(screen.getByLabelText('企业名称（可选）'), {
      target: { value: '启航科技' },
    })
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    await waitFor(() => expect(screen.getByText('用户名已存在')).toBeInTheDocument())
    expect(screen.getByLabelText('用户名')).toHaveValue('alice')
    expect(screen.getByLabelText('密码')).toHaveValue('Pass1234')
    expect(screen.getByLabelText('企业名称（可选）')).toHaveValue('启航科技')
  })

  it('1001 → 表单行内错误，已填内容不丢失', async () => {
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/register',
        method: 'post',
        code: 1001,
        message: '参数非法',
      }),
    )
    renderAt('/register')
    fillCredentials('alice', 'Pass1234')
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    await waitFor(() =>
      expect(screen.getByText('输入有误，请按提示修改后重试')).toBeInTheDocument(),
    )
    expect(screen.getByLabelText('用户名')).toHaveValue('alice')
    expect(screen.getByLabelText('密码')).toHaveValue('Pass1234')
  })
})
