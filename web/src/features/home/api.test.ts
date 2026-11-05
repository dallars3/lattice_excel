import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import {
  DEFAULT_BASE_NAME,
  createBase,
  deleteBase,
  duplicateBase,
  fetchHome,
  renameBase,
  searchBases,
  toggleStar,
} from './api'

/** H-21 形状：encoding/json 直接返回，last_modified_at 是 number。 */
const RAW_HOME = {
  recent_list: [
    {
      id: 'base_recent',
      name: '设计素材库',
      table_count: 3,
      my_role: 'owner',
      starred: false,
      last_modified_at: 1730000000000,
      last_modifier: 'Alice',
    },
  ],
  created_list: [
    {
      id: 'base_created',
      name: '项目排期',
      table_count: 2,
      my_role: 'manager',
      starred: true,
      last_modified_at: 1730000001000,
      last_modifier: 'Bob',
    },
  ],
  shared_list: [],
  star_list: [
    {
      id: 'base_star',
      name: '客户跟进',
      table_count: 1,
      my_role: 'editor',
      starred: true,
      last_modified_at: 1730000002000,
      last_modifier: 'Carol',
    },
  ],
  invitation_list: [],
}

/** H-22/H-23/H-24/H-27 形状：protojson → int64 为字符串。 */
const RAW_SEARCH = {
  base_list: [
    {
      id: 'base_search',
      name: '设计素材库',
      table_count: 3,
      my_role: 'viewer',
      starred: false,
      last_modified_at: '1730000000000',
      last_modifier: 'Alice',
    },
  ],
}

function rawBase(id: string, name: string, lastModifiedAt: string | number) {
  return {
    id,
    name,
    table_count: 1,
    my_role: 'editor',
    starred: false,
    last_modified_at: lastModifiedAt,
    last_modifier: 'Alice',
  }
}

describe('home api（H-21 聚合）', () => {
  it('四列表原样归一为 camelCase；number 型 last_modified_at 保持 number', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/home', data: RAW_HOME }))
    const home = await fetchHome()

    expect(home.recentList).toHaveLength(1)
    expect(home.createdList[0]).toMatchObject({
      id: 'base_created',
      name: '项目排期',
      tableCount: 2,
      myRole: 'manager',
      starred: true,
      lastModifiedAt: 1730000001000,
      lastModifier: 'Bob',
    })
    expect(home.starList[0].lastModifiedAt).toBe(1730000002000)
    expect(home.sharedList).toEqual([])
  })
})

describe('home api（H-22 搜索）', () => {
  it('携带 q；protojson 的字符串时间戳归一为 number', async () => {
    let query = ''
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/bases',
        onRequest: (request) => {
          query = new URL(request.url).searchParams.get('q') ?? ''
        },
        data: RAW_SEARCH,
      }),
    )
    const list = await searchBases('设计')
    expect(query).toBe('设计')
    expect(list[0].lastModifiedAt).toBe(1730000000000)
    expect(list[0].myRole).toBe('viewer')
  })
})

describe('home api 写请求（后端严格 JSON，不注入 client_request_id）', () => {
  it('H-23 新建：空名使用默认名称，body 仅 name', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/bases',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { base: rawBase('base_new', DEFAULT_BASE_NAME, '1730000003000') },
      }),
    )
    const base = await createBase('')
    expect(body).toEqual({ name: DEFAULT_BASE_NAME })
    expect(base).toMatchObject({ id: 'base_new', lastModifiedAt: 1730000003000 })
  })

  it('H-24 重命名：PATCH body 仅 name，不含 client_request_id', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id',
        method: 'patch',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { base: rawBase('base_1', '新名称', '1730000004000') },
      }),
    )
    await renameBase('base_1', '新名称')
    expect(body).toEqual({ name: '新名称' })
  })

  it('H-25 复制：POST 无请求体（不注入 client_request_id）', async () => {
    let rawBody = 'unset'
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/duplicate',
        method: 'post',
        onRequest: async (request) => {
          rawBody = await request.text()
        },
        data: { base: rawBase('base_copy', '副本', '1730000005000') },
      }),
    )
    const base = await duplicateBase('base_1')
    expect(rawBody).toBe('')
    expect(base.id).toBe('base_copy')
  })

  it('H-26 删除：DELETE body 仅 confirm=true', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id',
        method: 'delete',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: {},
      }),
    )
    await deleteBase('base_1')
    expect(body).toEqual({ confirm: true })
  })

  it('H-27 星标：POST body 仅 starred', async () => {
    let body: Record<string, unknown> = {}
    mswServer.use(
      mockEnvelope({
        path: '*/api/v1/bases/:base_id/star',
        method: 'post',
        onRequest: async (request) => {
          body = (await request.json()) as Record<string, unknown>
        },
        data: { base: rawBase('base_1', '客户跟进', '1730000006000') },
      }),
    )
    const base = await toggleStar('base_1', true)
    expect(body).toEqual({ starred: true })
    expect(base.lastModifiedAt).toBe(1730000006000)
  })
})
