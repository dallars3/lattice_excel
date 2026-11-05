// httpClient：全部后端交互的通信底座（技术方案 5.1）。
// - 统一解析 {code,message,data} 信封：code===0 透传 data；非 0 抛类型化 ApiError{code,message,meta}；
// - 会话票据：同源 Cookie 自动携带（credentials include）；备用 Authorization Bearer（注入 token getter，默认不启用）；
// - 写请求（POST/PUT/PATCH/DELETE）自动携带幂等 client_request_id：页内 map 防重复提交，失败重试复用同一 id；
// - VITE_API_BASE 经 lib/env.API_BASE 拼接，默认空串（同源相对路径）。
import { API_BASE } from '@/lib/env'

import { lookupErrorCatalog, type CatalogEntry } from './error-catalog'
import { defaultIdempotency } from './idempotency'

/** 信封形状（接口文档 8.0.1）。 */
export interface Envelope<T = unknown> {
  readonly code: number
  readonly message: string
  readonly data: T
}

/**
 * 类型化业务错误：catalog 条目与 error-catalog 同源（组件直接取文案/动作/恢复策略）。
 * meta 携带信封 data（如 4001 的 {their_value, their_version}，供冲突弹窗三动作裁决）。
 */
export class ApiError extends Error {
  readonly code: number
  readonly meta: unknown
  readonly catalog: CatalogEntry

  constructor(code: number, message: string, meta: unknown, catalog: CatalogEntry) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.meta = meta
    this.catalog = catalog
  }
}

/** 类型守卫：catch 块里安全收窄到 ApiError。 */
export function isApiError(value: unknown): value is ApiError {
  return value instanceof ApiError
}

/** 非 2xx/非信封/网络失败的合成兜底码：服务暂不可用（全局层、retry）。 */
const TRANSPORT_FAILURE_CODE = 9001

function isEnvelope(value: unknown): value is Envelope {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as Envelope).code === 'number' &&
    typeof (value as Envelope).message === 'string' &&
    'data' in (value as object)
  )
}

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export interface QueryParams {
  [key: string]: string | number | undefined
}

export interface RequestOptions {
  method?: HttpMethod
  query?: QueryParams
  /** 请求体：写请求为业务对象（自动并入 client_request_id）；GET 省略。 */
  body?: unknown
  signal?: AbortSignal
  /** 显式幂等键；缺省时写请求按 method+url+body 派生（页内防重复提交 + 失败重试复用 id）。 */
  idempotencyKey?: string
  headers?: Record<string, string>
}

/** token getter 注入位（Bearer 备用通道）：默认不启用，仅同源 Cookie 会话。 */
let tokenGetter: (() => string | null | undefined) | null = null

/** 设置/复位 Bearer token 来源（复位传 null）。 */
export function setAuthTokenGetter(getter: (() => string | null | undefined) | null): void {
  tokenGetter = getter
}

/** 拼接基地址、路径与 query（纯函数，供表驱动单测）。 */
export function buildUrl(
  base: string,
  path: string,
  query?: QueryParams,
): string {
  const url = `${base}${path}`
  if (!query) return url
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined) continue
    params.append(key, String(value))
  }
  const encoded = params.toString()
  return encoded === '' ? url : `${url}?${encoded}`
}

const WRITE_METHODS: readonly HttpMethod[] = ['POST', 'PUT', 'PATCH', 'DELETE']

function stableBodyKey(body: unknown): string {
  if (body === undefined) return ''
  if (typeof body === 'string') return body
  return JSON.stringify(body)
}

async function sendEnvelopeRequest(
  url: string,
  options: RequestOptions,
  clientRequestId?: string,
): Promise<Response> {
  const method = options.method ?? 'GET'
  const headers: Record<string, string> = { ...options.headers }
  let body: unknown = options.body
  if (clientRequestId !== undefined && (body === undefined || typeof body === 'object')) {
    body = body === undefined ? {} : { ...(body as Record<string, unknown>) }
    ;(body as Record<string, unknown>).client_request_id = clientRequestId
  }
  if (body !== undefined) {
    headers['content-type'] = 'application/json'
  }
  const token = tokenGetter?.() ?? null
  if (token !== null && token !== '') {
    headers.authorization = `Bearer ${token}`
  }
  return fetch(url, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'include',
    signal: options.signal,
  })
}

async function parseEnvelope<T>(response: Response): Promise<T> {
  let payload: unknown
  try {
    payload = await response.json()
  } catch {
    throw new ApiError(
      TRANSPORT_FAILURE_CODE,
      '响应不是有效数据（可能为网关错误页）',
      null,
      lookupErrorCatalog(TRANSPORT_FAILURE_CODE),
    )
  }
  if (!isEnvelope(payload)) {
    throw new ApiError(
      TRANSPORT_FAILURE_CODE,
      '响应缺少统一信封（code/message/data）',
      null,
      lookupErrorCatalog(TRANSPORT_FAILURE_CODE),
    )
  }
  if (payload.code === 0) {
    return payload.data as T
  }
  throw new ApiError(payload.code, payload.message, payload.data, lookupErrorCatalog(payload.code))
}

/** 非 ApiError 的底层异常（网络失败/中断等）统一包装为 9001 传输失败；ApiError（信封码/解析兜底）原样透传。 */
async function withTransportFailure<T>(run: () => Promise<T>): Promise<T> {
  try {
    return await run()
  } catch (error) {
    if (isApiError(error)) throw error
    throw new ApiError(
      TRANSPORT_FAILURE_CODE,
      `网络请求失败：${error instanceof Error ? error.message : String(error)}`,
      null,
      lookupErrorCatalog(TRANSPORT_FAILURE_CODE),
    )
  }
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const url = buildUrl(API_BASE, path, options.query)
  const isWrite = WRITE_METHODS.includes(method)
  if (!isWrite) {
    return withTransportFailure(async () => {
      const response = await sendEnvelopeRequest(url, options)
      return await parseEnvelope<T>(response)
    })
  }
  // 写请求：页内幂等（在途去重 + 失败重试复用同一 client_request_id）。
  const idempotencyKey =
    options.idempotencyKey ?? `${method} ${url} ${stableBodyKey(options.body)}`
  return defaultIdempotency.run(idempotencyKey, async (clientRequestId) => {
    return withTransportFailure(async () => {
      const response = await sendEnvelopeRequest(url, options, clientRequestId)
      return await parseEnvelope<T>(response)
    })
  })
}

/** httpClient 门面：request 为通用入口，method 便捷方法按语义收敛。 */
export const http = {
  request,
  get: <T>(path: string, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'GET' }),
  post: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'POST', body }),
  put: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'PUT', body }),
  patch: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'PATCH', body }),
  delete: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    request<T>(path, { ...options, method: 'DELETE', body }),
}
