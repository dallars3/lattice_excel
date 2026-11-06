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
 * 游标分页假接口（H-48 语义）：data = { record_list, has_more, next_cursor,
 * unsolved_comment_count_map }，外包信封（与真实后端出参对齐：字段恒在）。
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
        unsolved_comment_count_map: {},
      },
    })
  })
}

export interface MockRecordsPageOptions {
  /** 记录查询路由，如 '/api/v1/bases/:id/records'。 */
  path: string
  /** 命中时回调（断言请求参数用）。 */
  onRequest?: (request: Request) => void
}

/**
 * 真实后端「修复后」空页信封（票 01 H-48）：网关 EmitDefaultValues 后，
 * 空表恒返回 record_list:[] 与 has_more/next_cursor/unsolved_comment_count_map
 * （零值以 []/""/false/{}/0 呈现，不再塌缩成 data:{}）。
 */
export function mockEmptyRecordsPage(options: MockRecordsPageOptions): RequestHandler {
  const pattern = options.path.startsWith('/') ? `*${options.path}` : options.path
  return http.get(pattern, ({ request }) => {
    options.onRequest?.(request)
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: {
        record_list: [],
        has_more: false,
        next_cursor: '',
        unsolved_comment_count_map: {},
      },
    })
  })
}

/**
 * 真实后端「修复前」空页信封（票 01 回归锁）：protojson 缺省省略零值字段，
 * 空表返回 data:{}（record_list/has_more/next_cursor 全部缺失）。
 * 组件测试用例断言 extractCursorPage 照旧抛「游标分页契约违背」（tripwire 不放宽），
 * 网格呈错误横幅而非静默空表。
 */
export function mockEmptyRecordsPageMissingList(
  options: MockRecordsPageOptions,
): RequestHandler {
  const pattern = options.path.startsWith('/') ? `*${options.path}` : options.path
  return http.get(pattern, ({ request }) => {
    options.onRequest?.(request)
    return HttpResponse.json({ code: 0, message: 'ok', data: {} })
  })
}

export interface MockCollaboratorEntry {
  /** 条目稳定 id（协作者记录主键在 Base 内的寻址键，等于 user_id）。 */
  id: string
  user_id: string
  username: string
  display_name: string
  role: 'viewer' | 'filler' | 'editor' | 'manager'
  internal: boolean
  source: 'invite' | 'link'
  granted_at: number
}

export interface MockCollaboratorMembersOptions {
  /** 协作者列表路由，如 '/api/v1/bases/:base_id/shares/members'。 */
  path: string
  /** 条目集：缺省给「H-63 真实形状」样例（内部+外部条目，真实用户名/显示名）。 */
  collaborator_list?: MockCollaboratorEntry[]
  /** 命中时回调（断言请求参数用）。 */
  onRequest?: (request: Request) => void
}

/** 「H-63 真实形状」样例条目：外部条目 username/display_name 为真实值（冒烟缺陷回归锁：不得落 user_id）。 */
export function mockCollaboratorEntrySample(
  overrides: Partial<MockCollaboratorEntry> = {},
): MockCollaboratorEntry {
  return {
    id: 'usr_external_9',
    user_id: 'usr_external_9',
    username: 'dave',
    display_name: '戴夫',
    role: 'filler',
    internal: false,
    source: 'invite',
    granted_at: 1730000000000,
    ...overrides,
  }
}

/**
 * 协作者列表假接口（H-63 真实形状）：data = { collaborator_list: [...] }，
 * 条目恒携带 id/user_id/username/display_name/role/internal/source/granted_at
 * （与网关 EmitDefaultValues 出参对齐，零值不塌缩）。
 */
export function mockCollaboratorMembers(
  options: MockCollaboratorMembersOptions,
): RequestHandler {
  const pattern = options.path.startsWith('/') ? `*${options.path}` : options.path
  return http.get(pattern, ({ request }) => {
    options.onRequest?.(request)
    const collaboratorList = options.collaborator_list ?? [
      mockCollaboratorEntrySample(),
      mockCollaboratorEntrySample({
        id: 'usr_internal_1',
        user_id: 'usr_internal_1',
        username: 'owner',
        display_name: '所有者',
        role: 'manager',
        internal: true,
      }),
    ]
    return HttpResponse.json({
      code: 0,
      message: 'ok',
      data: { collaborator_list: collaboratorList },
    })
  })
}
