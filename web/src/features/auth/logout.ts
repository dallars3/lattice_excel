import type { QueryClient } from '@tanstack/react-query'

import {
  authStore,
  editSessionStore,
  presenceStore,
  selectionStore,
  uiStore,
  viewConfigDraftStore,
} from '@/stores'

import { logoutAccount } from './api'

/** 清空会话、Query 缓存与全部本地编辑态（共享设备登出后不残留任何上下文）。 */
export function clearLocalSession(queryClient: QueryClient): void {
  authStore.getState().clear()
  editSessionStore.setState({ editingCell: null, snapshotBeforeEdit: null, version: 0 })
  selectionStore.setState({ focusedCell: null, selectedRange: null })
  presenceStore.setState({ onlineMembers: [], otherCursors: {}, editingBy: {} })
  viewConfigDraftStore.setState({ isDirty: false, draft: null })
  uiStore.setState({ bannerMessage: null, modalStack: [], undoDeadlineAt: null })
  queryClient.clear()
}

/** H-03 登出 + 本地清理：服务端会话已失效/网络异常时也照常清理。 */
export async function logoutAndClear(queryClient: QueryClient): Promise<void> {
  try {
    await logoutAccount()
  } catch {
    // 登出失败不阻断本地清理（Cookie 失效/网络异常均不应让用户困在原地）。
  } finally {
    clearLocalSession(queryClient)
  }
}
