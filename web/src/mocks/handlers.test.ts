// H-63 真实形状 fixture 契约锁：协作者条目恒携带稳定 id 与真实 username/display_name
// （冒烟缺陷：外部条目三字段均落 user_id、条目无 id 字段 → H-65/H-66 无法寻址）。
// 协作者管理 UI 属 M4 占位，先用 httpClient 消费 fixture 锁形状，UI 落地后可直接复用。
import { describe, expect, it } from 'vitest'

import { http } from '@/core/http/client'
import { mockCollaboratorEntrySample, mockCollaboratorMembers } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

const PATH = '/api/v1/bases/b_1/shares/members'

describe('mocks/handlers mockCollaboratorMembers（H-63 真实形状）', () => {
  it('条目携带稳定 id 且外部条目 username/display_name 为真实值（不落 user_id）', async () => {
    mswServer.use(mockCollaboratorMembers({ path: PATH }))
    const data = await http.get<{ collaborator_list: Array<Record<string, unknown>> }>(PATH)
    const entries = data.collaborator_list
    expect(entries.length).toBe(2)
    for (const entry of entries) {
      expect(entry.id).toBeTruthy()
      expect(entry.id).toBe(entry.user_id)
      expect(typeof entry.username).toBe('string')
      expect((entry.username as string).length).toBeGreaterThan(0)
      expect(entry.username).not.toBe(entry.user_id)
      expect((entry.display_name as string).length).toBeGreaterThan(0)
      expect(entry.display_name).not.toBe(entry.user_id)
      expect(['viewer', 'filler', 'editor', 'manager']).toContain(entry.role)
      expect(typeof entry.internal).toBe('boolean')
      expect(['invite', 'link']).toContain(entry.source)
      expect(typeof entry.granted_at).toBe('number')
    }
    const external = entries.find((entry) => entry.internal === false)
    expect(external).toMatchObject({
      id: 'usr_external_9',
      user_id: 'usr_external_9',
      username: 'dave',
      display_name: '戴夫',
      role: 'filler',
      source: 'invite',
    })
  })

  it('自定义条目透传（filler 授权矩阵场景可复用）', async () => {
    mswServer.use(
      mockCollaboratorMembers({
        path: PATH,
        collaborator_list: [
          mockCollaboratorEntrySample({ username: 'ed2', display_name: '填报员二号', role: 'filler' }),
        ],
      }),
    )
    const data = await http.get<{ collaborator_list: Array<Record<string, unknown>> }>(PATH)
    expect(data.collaborator_list[0]).toMatchObject({
      username: 'ed2',
      display_name: '填报员二号',
      role: 'filler',
    })
  })
})
