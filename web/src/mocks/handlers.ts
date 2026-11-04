// MSW handler 工厂（票 04 建，票 09 复用）：对任意后端路由注册 handler，
// 可返回指定错误码的信封，或带真实游标语义的假记录分页接口。
import { HttpResponse, http, type RequestHandler } from 'msw'

import { lookupErrorCatalog } from '@/core/http/error-catalog'

export type MockMethod = 'get' | 'post' | 'put' | 'patch' | 'delete'

export interface MockEnvelopeOptions {
  /** 路由路径，如 '/api/v1/auth/me'（以 / 开头时按任意 origin 匹配）。 */
  path: string
  method?: MockMethod
  /** 信封 code：0 成功透传 data；非 0 由 httpClient 抛 ApiError。 */
  code?: number
  /** 缺省时非 0 码取 error-catalog 文案。 */
  message?: string
  /** 信封 data：成功即业务数据；错误即 ApiError.meta（如 4001 的 their_value/their_version）。 */
  data?: unknown
  /** HTTP 状态缺省：code===0 → 200，否则取目录里的对应 HTTP 状态。 */
  status?: number
  /** 命中时回调（测试断言请求体/头用）。 */
  onRequest?: (request: Request) => void
}

/** 信封 handler 工厂：mockEnvelope({ path, code: 4001, data: { their_value: 99 } })。 */
export function mockEnvelope(options: MockEnvelopeOptions): RequestHandler {
  const method = options.method ?? 'get'
  const code = options.code ?? 0
  const message =
    options.message ?? (code === 0 ? 'ok' : lookupErrorCatalog(code).title)
  const status = options.status ?? (code === 0 ? 200 : lookupErrorCatalog(code).httpStatus)
  const pattern = options.path.startsWith('/') ? `*${options.path}` : options.path
  const responder = async ({ request }: { request: Request }): Promise<Response> => {
    options.onRequest?.(request)
    return HttpResponse.json({ code, message, data: options.data ?? null }, { status })
  }
  return http[method](pattern, responder)
}

export interface MockCursorRow {
  id: string
  cells_map?: Record<string, unknown>
  [key: string]: unknown
}

export interface MockCursorOptions {
  /** 记录查询路由，如 '/api/v1/bases/:id/records'。 */
  path: string
  /** 全量行集：handler 按 cursor（cur_<offset>）+ limit 切片。 */
  rows: MockCursorRow[]
  /** 上限保护：请求 limit 超 100 时按 100 截断（接口文档 8.0.2）。 */
  maxLimit?: number
  /** 命中时回调（断言分页参数用）。 */
  onRequest?: (request: Request) => void
}

/**
 * 游标分页假接口（H-48 语义）：data = { record_list, has_more, next_cursor }，外包信封。
 * 游标方案：cursor='cur_<offset>' 编码偏移量；空/缺省 cursor 从第 0 行起。
 */
export function mockCursorRecords(options: MockCursorOptions): RequestHandler {
  const pattern = options.path.startsWith('/') ? `*${options.path}` : options.path
  const maxLimit = options.maxLimit ?? 100
  return http.get(pattern, ({ request }) => {
    options.onRequest?.(request)
    const url = new URL(request.url)
    const rawCursor = url.searchParams.get('cursor') ?? ''
    const offset = Number.parseInt(rawCursor.replace(/^cur_/, ''), 10) || 0
    const limit = Math.min(Number.parseInt(url.searchParams.get('limit') ?? '20', 10) || 20, maxLimit)
    const recordList = options.rows.slice(offset, offset + limit)
    const hasMore = offset + limit < options.rows.length
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: {
        record_list: recordList,
        has_more: hasMore,
        next_cursor: hasMore ? `cur_${offset + limit}` : null,
      },
    })
  })
}
