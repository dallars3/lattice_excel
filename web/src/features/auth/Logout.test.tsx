import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { AppProviders, appQueryClient } from '@/app/providers'
import { AppRoutes } from '@/app/router'
import { editHistoryStore } from '@/features/records/edit-history'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import {
  authStore,
  editSessionStore,
  presenceStore,
  selectionStore,
  uiStore,
  viewConfigDraftStore,
  workspaceStore,
} from '@/stores'

const ME = {
  user: { id: 'usr_1', username: 'alice', enterprise_id: 'ent_1' },
  enterprise: { id: 'ent_1', name: '启航科技', owner_id: 'usr_1', member_count: 1 },
  my_role: 'owner',
}

const HOME = {
  recent_list: [],
  created_list: [],
  shared_list: [],
  star_list: [],
  invitation_list: [],
}

describe('登出（票 01）', () => {
  beforeEach(() => {
    authStore.getState().clear()
    appQueryClient.clear()
    editHistoryStore.getState().clear()
    workspaceStore.setState({
      forcedReadOnly: false,
      offline: false,
      inFlightWriteCount: 0,
      fieldDescriptions: {},
      fileMetaMap: {},
    })
  })

  it('调用 H-03 后清空 Query 缓存与本地编辑态，回到登录页', async () => {
    let logoutBody = 'unset'
    mswServer.use(
      mockEnvelope({ path: '/api/v1/auth/me', data: ME }),
      mockEnvelope({ path: '/api/v1/home', data: HOME }),
      mockEnvelope({
        path: '/api/v1/auth/logout',
        method: 'post',
        onRequest: async (request) => {
          logoutBody = await request.text()
        },
        data: {},
      }),
    )
    render(
      <AppProviders>
        <MemoryRouter initialEntries={['/home']}>
          <AppRoutes />
        </MemoryRouter>
      </AppProviders>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('协作多维表格'),
    )

    // 预置服务端缓存与本地编辑态，验证登出后全部清空。
    appQueryClient.setQueryData(['home'], { recent_list: [] })
    editSessionStore.setState({
      editingCell: { row: 0, col: 0 },
      snapshotBeforeEdit: 'draft',
      version: 7,
    })
    selectionStore.setState({
      focusedCell: { row: 0, col: 0 },
      selectedRange: null,
    })
    presenceStore.setState({
      onlineMembers: [{ userId: 'usr_2', colorIndex: 1 }],
      otherCursors: {},
      editingBy: {},
    })
    viewConfigDraftStore.setState({ isDirty: true, draft: { filter_list: [] } })
    uiStore.setState({ bannerMessage: '保存中', modalStack: ['m'], undoDeadlineAt: 1 })
    editHistoryStore.getState().push({
      baseId: 'base_1',
      recordId: 'rec_1',
      fieldId: 'fld_1',
      before: '旧值',
      after: '新值',
      versionAtEdit: 1,
    })
    workspaceStore.setState({
      forcedReadOnly: true,
      offline: true,
      inFlightWriteCount: 2,
      fieldDescriptions: { fld_1: '标题说明' },
      fileMetaMap: { fil_1: { name: '附件.pdf', displayName: '附件.pdf' } },
    })

    fireEvent.click(screen.getByRole('button', { name: '退出登录' }))

    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('登录'),
    )
    expect(logoutBody).not.toContain('client_request_id')
    expect(authStore.getState().session).toBeNull()
    // queryClient.clear() 后不残留任何服务端数据（守卫的 me observer 可能重建一个空 query）。
    expect(appQueryClient.getQueryData(['home'])).toBeUndefined()
    expect(
      appQueryClient.getQueryCache().getAll().filter((query) => query.state.data !== undefined),
    ).toHaveLength(0)
    expect(editSessionStore.getState()).toMatchObject({
      editingCell: null,
      snapshotBeforeEdit: null,
      version: 0,
    })
    expect(selectionStore.getState()).toMatchObject({
      focusedCell: null,
      selectedRange: null,
    })
    expect(presenceStore.getState().onlineMembers).toEqual([])
    expect(viewConfigDraftStore.getState()).toMatchObject({ isDirty: false, draft: null })
    expect(uiStore.getState()).toMatchObject({
      bannerMessage: null,
      modalStack: [],
      undoDeadlineAt: null,
    })
    expect(editHistoryStore.getState().entries).toEqual([])
    expect(workspaceStore.getState()).toMatchObject({
      forcedReadOnly: false,
      offline: false,
      inFlightWriteCount: 0,
      fieldDescriptions: {},
      fileMetaMap: {},
    })
  })
})
