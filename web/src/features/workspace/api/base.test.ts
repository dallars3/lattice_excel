import { describe, expect, it } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import { fetchBaseById } from './base'

function rawBase(id: string, name: string, myRole = 'owner') {
  return {
    id,
    name,
    table_count: 2,
    my_role: myRole,
    starred: false,
    last_modified_at: 1730000000000,
    last_modifier: 'Alice',
  }
}

function mockHome(lists: Record<string, unknown[]>) {
  mswServer.use(
    mockEnvelope({
      path: '/api/v1/home',
      data: {
        recent_list: [],
        created_list: [],
        shared_list: [],
        star_list: [],
        invitation_list: [],
        ...lists,
      },
    }),
  )
}

describe('workspace base api（契约无 GET /bases/{id}，经 H-21 四列表兜底查找）', () => {
  it('在四列表中按 id 查找并归一为 BaseSummary', async () => {
    mockHome({
      recent_list: [rawBase('base_recent', '最近', 'editor')],
      shared_list: [rawBase('base_shared', '共享', 'viewer')],
    })

    const base = await fetchBaseById('base_shared')

    expect(base).toMatchObject({
      id: 'base_shared',
      name: '共享',
      tableCount: 2,
      myRole: 'viewer',
      lastModifiedAt: 1730000000000,
      lastModifier: 'Alice',
    })
  })

  it('同一 Base 出现在多个列表时仍返回同一 id 的 summary', async () => {
    mockHome({
      recent_list: [rawBase('base_same', '同一')],
      created_list: [rawBase('base_same', '同一')],
      star_list: [rawBase('base_same', '同一', 'manager')],
    })

    const base = await fetchBaseById('base_same')

    expect(base?.id).toBe('base_same')
    expect(base?.myRole).toBe('owner')
  })

  it('四列表均无该 id 时返回 null（页面按无权限态兜底）', async () => {
    mockHome({ recent_list: [rawBase('base_other', '其他')] })

    expect(await fetchBaseById('base_missing')).toBeNull()
  })
})
