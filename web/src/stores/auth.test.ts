import { beforeEach, describe, expect, it } from 'vitest'

import { authStore } from './auth'

const USER = {
  id: 'usr_1',
  username: 'alice',
  display_name: 'Alice',
  enterprise_id: 'ent_1',
}

const ENTERPRISE = { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 3 }

describe('authStore 会话状态（票 01）', () => {
  beforeEach(() => {
    authStore.getState().clear()
  })

  it('初始无会话、未打开重登弹窗', () => {
    expect(authStore.getState().session).toBeNull()
    expect(authStore.getState().reloginOpen).toBe(false)
  })

  it('setSession 由企业归属派生 internal=true（邀请/建企业注册）', () => {
    authStore.getState().setSession({ user: USER, enterprise: ENTERPRISE, myRole: 'owner' })
    const session = authStore.getState().session
    expect(session).toEqual({ user: USER, enterprise: ENTERPRISE, myRole: 'owner', internal: true })
  })

  it('无企业且 user.enterprise_id 为空 → 外部身份 internal=false（链接注册）', () => {
    authStore.getState().setSession({
      user: { id: 'usr_2', username: 'carol', enterprise_id: '' },
    })
    expect(authStore.getState().session?.internal).toBe(false)
  })

  it('openRelogin/closeRelogin 切换弹窗；clear 同时清会话与弹窗', () => {
    authStore.getState().openRelogin()
    expect(authStore.getState().reloginOpen).toBe(true)
    authStore.getState().closeRelogin()
    expect(authStore.getState().reloginOpen).toBe(false)

    authStore.getState().setSession({ user: USER, enterprise: ENTERPRISE })
    authStore.getState().openRelogin()
    authStore.getState().clear()
    expect(authStore.getState().session).toBeNull()
    expect(authStore.getState().reloginOpen).toBe(false)
  })
})
