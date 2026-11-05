import { create } from 'zustand'

import type { AuthEnterprise, AuthUser } from '@/features/auth/types'

/** 写入会话的输入：internal 由身份归属派生，不接受调用方臆造。 */
export interface AuthSessionInput {
  user: AuthUser
  enterprise?: AuthEnterprise
  myRole?: string
}

/** 会话快照：身份事实源为 H-04 me；internal = 有企业归属（无企业/链接注册视为外部）。 */
export interface AuthSession extends AuthSessionInput {
  internal: boolean
}

export interface AuthState {
  session: AuthSession | null
  /** 任意接口回包 2001 时由全局桥打开的重新登录弹窗。 */
  reloginOpen: boolean
  setSession: (input: AuthSessionInput) => void
  clear: () => void
  openRelogin: () => void
  closeRelogin: () => void
}

function deriveInternal(input: AuthSessionInput): boolean {
  return Boolean(input.enterprise?.id ?? input.user.enterprise_id)
}

export const authStore = create<AuthState>()((set) => ({
  session: null,
  reloginOpen: false,
  setSession: (input) =>
    set({ session: { ...input, internal: deriveInternal(input) } }),
  clear: () => set({ session: null, reloginOpen: false }),
  openRelogin: () => set({ reloginOpen: true }),
  closeRelogin: () => set({ reloginOpen: false }),
}))
