// 认证接口（H-01~H-04）：后端 decodeJSONBody 使用 DisallowUnknownFields，
// 这三个写接口不接收 client_request_id，因此全部 attachClientRequestId:false。
import { http } from '@/core/http'

import type { AuthCredential, AuthMeData } from './types'

export interface RegisterPayload {
  username: string
  password: string
  /** 路径③（直接建企业）必填语义由表单决定；其余路径缺省不发送。 */
  enterprise_name?: string
  /** 路径②（持分享链接注册）。 */
  link_token?: string
}

export interface LoginPayload {
  username: string
  password: string
}

const STRICT_JSON_WRITE = { attachClientRequestId: false } as const

/** H-01 注册：成功 data = {token, user}；网关不设置会话 Cookie。 */
export function registerAccount(payload: RegisterPayload): Promise<AuthCredential> {
  return http.post<AuthCredential>('/api/v1/auth/register', payload, STRICT_JSON_WRITE)
}

/** H-02 登录：成功响应由网关设置同源 session Cookie。 */
export function loginAccount(payload: LoginPayload): Promise<AuthCredential> {
  return http.post<AuthCredential>('/api/v1/auth/login', payload, STRICT_JSON_WRITE)
}

/** H-03 登出：网关删除服务端会话并清 Cookie。 */
export function logoutAccount(): Promise<unknown> {
  return http.post<unknown>('/api/v1/auth/logout', undefined, STRICT_JSON_WRITE)
}

/** H-04 当前身份：会话事实源。 */
export function fetchMe(): Promise<AuthMeData> {
  return http.get<AuthMeData>('/api/v1/auth/me')
}

/** 登录 + me：身份写入只信任 me（票 01 认证事实源口径）。 */
export async function loginAndLoadMe(payload: LoginPayload): Promise<AuthMeData> {
  await loginAccount(payload)
  return fetchMe()
}

/** 注册 + 登录 + me：H-01 不设 Cookie，注册成功后用同用户名密码补 H-02 换取会话。 */
export async function registerAndSignIn(payload: RegisterPayload): Promise<AuthMeData> {
  await registerAccount(payload)
  await loginAccount({ username: payload.username, password: payload.password })
  return fetchMe()
}
