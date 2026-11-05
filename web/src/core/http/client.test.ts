import { HttpResponse, http as mswHttp } from 'msw'
import { describe, expect, it } from 'vitest'

import {
  ApiError,
  buildUrl,
  http,
  isApiError,
  setApiErrorObserver,
  setAuthTokenGetter,
} from './client'
import { ALL_ERROR_CODES, lookupErrorCatalog } from './error-catalog'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

const PATH = '/api/v1/echo'

describe('core/http client 信封解析', () => {
  it('code===0 透传 data（对象/数组/null 表驱动）', async () => {
    const dataCases: { name: string; data: unknown }[] = [
      { name: '对象', data: { id: 'rec_1', version: 7 } },
      { name: '数组', data: [{ id: 'rec_1' }] },
      { name: 'null', data: null },
    ]
    for (const c of dataCases) {
      mswServer.use(mockEnvelope({ path: PATH, code: 0, data: c.data }))
      const data = await http.get<unknown>(PATH)
      expect(data).toEqual(c.data)
    }
  })

  it.each(ALL_ERROR_CODES.map((code) => [code] as const))(
    '错误码 %i：抛 ApiError，携带 code/message/meta 且 catalog 条目与目录同源',
    async (code) => {
      mswServer.use(
        mockEnvelope({ path: PATH, code, message: 'mock-failure', data: { hint: 'h' } }),
      )
      const err = await http.get(PATH).then(
        () => null,
        (e: unknown) => e,
      )
      expect(err).toBeInstanceOf(ApiError)
      expect(isApiError(err)).toBe(true)
      const apiErr = err as ApiError
      expect(apiErr.code).toBe(code)
      expect(apiErr.message).toBe('mock-failure')
      expect(apiErr.meta).toEqual({ hint: 'h' })
      expect(apiErr.catalog).toBe(lookupErrorCatalog(code))
    },
  )

  it('4001 冲突的 data（their_value/their_version）原样进 meta', async () => {
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/records/rec_1/cells/fld_1',
        method: 'put',
        code: 4001,
        data: { their_value: 99, their_version: 8 },
      }),
    )
    const err = await http.put('/api/v1/records/rec_1/cells/fld_1', { value: 126 }).then(
      () => null,
      (e: unknown) => e,
    )
    expect(isApiError(err)).toBe(true)
    expect((err as ApiError).meta).toEqual({ their_value: 99, their_version: 8 })
    expect((err as ApiError).catalog.recovery).toBe('resolve-conflict')
  })

  it('响应不是 JSON 信封（网关 HTML）：合成 9001 ApiError', async () => {
    mswServer.use(
      mswHttp.get(
        `*${PATH}`,
        () =>
          new HttpResponse('<html>gateway</html>', {
            headers: { 'content-type': 'text/html' },
          }),
      ),
    )
    const err = await http.get(PATH).then(
      () => null,
      (e: unknown) => e,
    )
    expect(isApiError(err)).toBe(true)
    expect((err as ApiError).code).toBe(9001)
  })

  it('网络层失败（连接拒绝等）：合成 9001 ApiError', async () => {
    mswServer.use(mswHttp.get(`*${PATH}`, () => HttpResponse.error()))
    const err = await http.get(PATH).then(
      () => null,
      (e: unknown) => e,
    )
    expect(isApiError(err)).toBe(true)
    expect((err as ApiError).code).toBe(9001)
    expect((err as ApiError).catalog.layer).toBe('global')
  })

  it('query 参数序列化：undefined 值剔除，其余进 searchParams', async () => {
    let seenUrl = ''
    mswServer.use(
      mockEnvelope({ path: PATH, onRequest: (request) => (seenUrl = request.url) }),
    )
    await http.get(PATH, { query: { table_id: 'tbl_1', view_id: undefined, limit: 50 } })
    const search = new URL(seenUrl).searchParams
    expect(search.get('table_id')).toBe('tbl_1')
    expect(search.get('limit')).toBe('50')
    expect(search.has('view_id')).toBe(false)
  })

  it('fetch 以 credentials include 发出（同源 Cookie 自动携带会话票据）', async () => {
    mswServer.use(mockEnvelope({ path: PATH, code: 0, data: null }))
    const originalFetch = globalThis.fetch
    let capturedInit: RequestInit | undefined
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      capturedInit = init
      return originalFetch(input, init)
    }) as typeof fetch
    try {
      await http.get(PATH)
    } finally {
      globalThis.fetch = originalFetch
    }
    expect(capturedInit?.credentials).toBe('include')
  })

  it('注入 token getter 时携带 Authorization: Bearer；复位后不再携带', async () => {
    const seen: (string | null)[] = []
    mswServer.use(
      mockEnvelope({
        path: PATH,
        onRequest: (request) => seen.push(request.headers.get('authorization')),
      }),
    )
    setAuthTokenGetter(() => 'tok_123')
    await http.get(PATH)
    setAuthTokenGetter(null)
    await http.get(PATH)
    expect(seen).toEqual(['Bearer tok_123', null])
  })

  it('GET 请求不携带 client_request_id', async () => {
    let body: string | undefined
    mswServer.use(
      mockEnvelope({ path: PATH, onRequest: async (request) => (body = await request.text()) }),
    )
    await http.get(PATH)
    expect(body).toBe('')
  })
})

describe('core/http client 写请求幂等（client_request_id）', () => {
  it('POST 自动生成并携带 uuid v4 形态的 client_request_id，业务字段保留', async () => {
    const bodies: Record<string, unknown>[] = []
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/bases/b_1/records',
        method: 'post',
        onRequest: async (request) =>
          bodies.push((await request.json()) as Record<string, unknown>),
      }),
    )
    await http.post('/api/v1/bases/b_1/records', { cells_map: { fld_1: '新行' } })
    expect(bodies).toHaveLength(1)
    expect(bodies[0].client_request_id).toMatch(UUID_V4)
    expect(bodies[0].cells_map).toEqual({ fld_1: '新行' })
  })

  it('失败重试复用同一 client_request_id（9002 失败 → 重试成功，两次 id 一致）', async () => {
    const bodies: Record<string, unknown>[] = []
    let attempts = 0
    mswServer.use(
      mswHttp.post(`*/api/v1/records/batch`, async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        attempts += 1
        if (attempts === 1) {
          return HttpResponse.json(
            { code: 9002, message: 'mock-failure', data: null },
            { status: 500 },
          )
        }
        return HttpResponse.json(
          { code: 0, message: 'ok', data: { result_list: [] } },
          { status: 200 },
        )
      }),
    )
    const payload = { op_list: [{ value: 1 }] }
    const first = await http.post('/api/v1/records/batch', payload).then(
      () => 'ok',
      () => 'failed',
    )
    expect(first).toBe('failed')
    await http.post('/api/v1/records/batch', payload)
    expect(bodies).toHaveLength(2)
    expect(bodies[0].client_request_id).toMatch(UUID_V4)
    expect(bodies[0].client_request_id).toBe(bodies[1].client_request_id)
  })

  it('同 key 在途并发去重：双击双发只打一次网络请求', async () => {
    let hits = 0
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/records',
        method: 'post',
        onRequest: () => {
          hits += 1
        },
        data: { id: 'rec_new' },
      }),
    )
    const payload = { cells_map: { fld_1: 'a' } }
    const [r1, r2] = await Promise.all([
      http.post('/api/v1/records', payload),
      http.post('/api/v1/records', payload),
    ])
    expect(hits).toBe(1)
    expect(r1).toEqual(r2)
  })
})

describe('core/http client 写请求不带 client_request_id（严格 JSON 契约）', () => {
  it('attachClientRequestId:false 时不注入 client_request_id，业务字段保持原样', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/auth/login',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { token: 'sess_1' },
      }),
    )
    await http.post(
      '/api/v1/auth/login',
      { username: 'alice', password: 'Pass1234' },
      { attachClientRequestId: false },
    )
    expect(Object.keys(body).sort()).toEqual(['password', 'username'])
  })
})

describe('core/http client ApiError 观察者', () => {
  it('每次抛 ApiError 前通知观察者，且不改变抛出语义（错误照常抛出）', async () => {
    const seen: number[] = []
    setApiErrorObserver((error) => seen.push(error.code))
    mswServer.use(mockEnvelope({ path: PATH, code: 2001 }))
    try {
      const err = await http.get(PATH).then(
        () => null,
        (e: unknown) => e,
      )
      expect(isApiError(err)).toBe(true)
      expect((err as ApiError).code).toBe(2001)
      expect(seen).toEqual([2001])
    } finally {
      setApiErrorObserver(null)
    }
  })

  it('观察者抛错不影响业务错误传播', async () => {
    setApiErrorObserver(() => {
      throw new Error('observer failure')
    })
    mswServer.use(mockEnvelope({ path: PATH, code: 1001 }))
    try {
      const err = await http.get(PATH).then(
        () => null,
        (e: unknown) => e,
      )
      expect(isApiError(err)).toBe(true)
      expect((err as ApiError).code).toBe(1001)
    } finally {
      setApiErrorObserver(null)
    }
  })
})

describe('core/http buildUrl（纯函数）', () => {
  const cases: {
    name: string
    base: string
    path: string
    query?: Record<string, string | number | undefined>
    want: string
  }[] = [
    { name: '同源默认：base 空串直接拼相对路径', base: '', path: '/api/v1/records', want: '/api/v1/records' },
    { name: '带 API_BASE 前缀', base: '/gateway', path: '/api/v1/records', want: '/gateway/api/v1/records' },
    { name: 'query 编码进 URL', base: '', path: '/records', query: { cursor: 'cur_1', limit: 20 }, want: '/records?cursor=cur_1&limit=20' },
    { name: 'query undefined 值剔除', base: '', path: '/records', query: { a: undefined, b: 1 }, want: '/records?b=1' },
  ]
  it.each(cases.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    expect(buildUrl(c.base, c.path, c.query)).toBe(c.want)
  })
})
